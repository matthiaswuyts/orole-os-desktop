package jarvis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ElevenLabs is a minimal REST client for the Agents platform. Request shapes were checked against the
// official docs on 2026-10-14 (see docs/ notes in the task report).
type ElevenLabs struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
	// Scrub lists extra secrets (for example the relay secret) to redact from error strings.
	Scrub []string
}

type Voice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"` // ElevenLabs' text, else built from the labels
}

// TTSModel is a curated text-to-speech model (see curatedModels).
type TTSModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`
}

// curatedModels is the single source for the voice model choices, in display order. Models() offers only the ones
// the account's /v1/models actually lists.
var curatedModels = []TTSModel{
	{ID: "eleven_flash_v2_5", Name: "Flash v2.5", Description: "Fastest and cheapest (~75 ms), 32 languages including Dutch.", Recommended: true},
	{ID: "eleven_v3_conversational", Name: "v3 Conversational", Description: "Most natural and expressive, built for agents, 70+ languages including Dutch. A bit slower and pricier."},
	{ID: "eleven_turbo_v2_5", Name: "Turbo v2.5 (older)", Description: "Slightly richer than Flash but slower; ElevenLabs now recommends Flash for new projects."},
}

// multilingualModel is the TTS model used when none is chosen and the call is not in English: ElevenLabs' default for
// an agent may be an English-only model, which it refuses for a non-English agent.
const multilingualModel = "eleven_flash_v2_5"

// ttsModel is the model to request for a call language: the chosen one, else multilingualModel unless the call is in
// English ("" leaves ElevenLabs' default).
func ttsModel(lang, model string) string {
	if model == "" && lang != LangEnglish {
		return multilingualModel
	}
	return model
}

// AgentConfig describes the shared agent. Empty VoiceID/TTSModel are omitted from the request. Language is a call
// language (LangDutch, LangEnglish, LangAuto); empty means Dutch.
type AgentConfig struct {
	RelayURL, LLMSecret, VoiceID, TTSModel, Language string
}

type OutboundCall struct {
	AgentID, PhoneNumberID, To, CallID, BotID, VoiceID, TTSModel string
}

func (e *ElevenLabs) base() string {
	if e.BaseURL != "" {
		return strings.TrimRight(e.BaseURL, "/")
	}
	return "https://api.elevenlabs.io"
}

func (e *ElevenLabs) client() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (e *ElevenLabs) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("xi-api-key", e.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := e.client().Do(req)
	if err != nil {
		return fmt.Errorf("ElevenLabs %s %s: %s", method, path, e.scrub(err.Error()))
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		snippet := e.scrub(strings.TrimSpace(string(data[:min(len(data), 300)])))
		return fmt.Errorf("ElevenLabs %s %s: %d %s", method, path, res.StatusCode, snippet)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (e *ElevenLabs) scrub(s string) string {
	for _, secret := range append([]string{e.Key}, e.Scrub...) {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

// voiceDescription is ElevenLabs' own description, or a short line built from the labels, for example
// "Calm · female · British · narration".
func voiceDescription(description string, labels map[string]string) string {
	if d := strings.TrimSpace(description); d != "" {
		return d
	}
	var parts []string
	for _, k := range []string{"descriptive", "gender", "age", "accent", "use_case"} {
		if v := strings.TrimSpace(labels[k]); v != "" {
			parts = append(parts, strings.ReplaceAll(v, "_", " "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, " · ")
	r := []rune(out)
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}

func (e *ElevenLabs) Voices(ctx context.Context) ([]Voice, error) {
	var res struct {
		Voices []struct {
			VoiceID     string            `json:"voice_id"`
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Labels      map[string]string `json:"labels"`
		} `json:"voices"`
	}
	if err := e.do(ctx, "GET", "/v1/voices", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Voice, 0, len(res.Voices))
	for _, v := range res.Voices {
		out = append(out, Voice{ID: v.VoiceID, Name: v.Name, Description: voiceDescription(v.Description, v.Labels)})
	}
	return out, nil
}

// Models returns the curated models the account offers for text to speech, in curated order.
func (e *ElevenLabs) Models(ctx context.Context) ([]TTSModel, error) {
	var res []struct {
		ModelID string `json:"model_id"`
		TTS     bool   `json:"can_do_text_to_speech"`
	}
	if err := e.do(ctx, "GET", "/v1/models", nil, &res); err != nil {
		return nil, err
	}
	offered := map[string]bool{}
	for _, m := range res {
		if m.TTS {
			offered[m.ModelID] = true
		}
	}
	out := []TTSModel{}
	for _, m := range curatedModels {
		if offered[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// agentBody configures the shared agent: the brain is our relay (custom LLM) and the first turn comes from the LLM
// (first_message empty) so the daemon's cached opener is spoken. The end_call and voicemail_detection tools stay
// configured, but nothing relies on them: the daemon detects voicemail itself and hangs up through Twilio.
//
// The agent language follows the call language (speech recognition and voice): "nl" or "en". Auto starts in Dutch and
// adds ElevenLabs' language_detection system tool with an English preset, so the agent can switch when the owner
// speaks English (field names checked against the ElevenLabs language-detection docs on 2026-10-14).
func agentBody(c AgentConfig) map[string]any {
	lang := LangDutch
	if c.Language == LangEnglish {
		lang = LangEnglish
	}
	tools := map[string]any{
		"end_call": map[string]any{"name": "end_call", "params": map[string]any{"system_tool_type": "end_call"}},
		"voicemail_detection": map[string]any{
			"name":   "voicemail_detection",
			"params": map[string]any{"system_tool_type": "voicemail_detection", "voicemail_message": ""},
		},
	}
	// Without Auto the tool is sent as null (and the presets empty), so an update after switching away from Auto
	// removes them instead of leaving the old values merged in.
	tools["language_detection"] = nil
	if c.Language == LangAuto {
		tools["language_detection"] = map[string]any{"type": "system", "name": "language_detection", "description": "",
			"params": map[string]any{"system_tool_type": "language_detection"}}
	}
	conv := map[string]any{
		"agent": map[string]any{
			"first_message": "",
			"language":      lang,
			"prompt": map[string]any{
				"prompt": "You relay a phone call. Speak the assistant's replies exactly.",
				"llm":    "custom-llm",
				"custom_llm": map[string]any{
					"url":             strings.TrimRight(c.RelayURL, "/") + "/llm",
					"model_id":        "bot",
					"request_headers": map[string]string{"x-orole-relay": c.LLMSecret},
				},
				"built_in_tools": tools,
			},
		},
		"turn": map[string]any{"turn_eagerness": "eager"},
	}
	conv["language_presets"] = map[string]any{}
	if c.Language == LangAuto {
		// The detection tool switches to the languages listed here; the first message stays empty in each.
		conv["language_presets"] = map[string]any{LangEnglish: map[string]any{"overrides": map[string]any{"agent": map[string]any{"first_message": ""}}}}
	}
	tts := map[string]any{}
	if c.VoiceID != "" {
		tts["voice_id"] = c.VoiceID
	}
	if m := ttsModel(lang, c.TTSModel); m != "" {
		tts["model_id"] = m
	}
	if len(tts) > 0 {
		conv["tts"] = tts
	}
	return map[string]any{
		"name":                "Jarvis",
		"conversation_config": conv,
		"platform_settings": map[string]any{
			"overrides": map[string]any{
				"conversation_config_override":                            map[string]any{"tts": map[string]any{"voice_id": true, "model_id": true}},
				"custom_llm_extra_body":                                   true,
				"enable_conversation_initiation_client_data_from_webhook": true,
			},
		},
	}
}

func (e *ElevenLabs) CreateAgent(ctx context.Context, c AgentConfig) (string, error) {
	var res struct {
		AgentID string `json:"agent_id"`
	}
	if err := e.do(ctx, "POST", "/v1/convai/agents/create", agentBody(c), &res); err != nil {
		return "", err
	}
	if res.AgentID == "" {
		return "", fmt.Errorf("ElevenLabs returned no agent id")
	}
	return res.AgentID, nil
}

func (e *ElevenLabs) UpdateAgent(ctx context.Context, id string, c AgentConfig) error {
	return e.do(ctx, "PATCH", "/v1/convai/agents/"+url.PathEscape(id), agentBody(c), nil)
}

func (e *ElevenLabs) ImportTwilioNumber(ctx context.Context, number, label, sid, token string) (string, error) {
	var res struct {
		ID string `json:"phone_number_id"`
	}
	err := e.do(ctx, "POST", "/v1/convai/phone-numbers", map[string]any{
		"provider": "twilio", "phone_number": number, "label": label, "sid": sid, "token": token,
	}, &res)
	if err != nil {
		return "", err
	}
	if res.ID == "" {
		return "", fmt.Errorf("ElevenLabs returned no phone number id")
	}
	return res.ID, nil
}

func (e *ElevenLabs) AssignAgent(ctx context.Context, phoneNumberID, agentID string) error {
	return e.do(ctx, "PATCH", "/v1/convai/phone-numbers/"+url.PathEscape(phoneNumberID), map[string]any{"agent_id": agentID}, nil)
}

// CreatePostCallWebhook registers an HMAC workspace webhook. ElevenLabs generates the signing secret and returns
// it once (it cannot be supplied); the caller must store it.
func (e *ElevenLabs) CreatePostCallWebhook(ctx context.Context, webhookURL, llmSecret string) (id, secret string, err error) {
	var res struct {
		ID     string `json:"webhook_id"`
		Secret string `json:"webhook_secret"`
	}
	err = e.do(ctx, "POST", "/v1/workspace/webhooks", map[string]any{
		"settings": map[string]any{
			"auth_type":       "hmac",
			"name":            "Jarvis post-call",
			"webhook_url":     webhookURL,
			"request_headers": map[string]string{"x-orole-relay": llmSecret},
		},
	}, &res)
	return res.ID, res.Secret, err
}

// SetWebhooks points the workspace's inbound personalization webhook at the relay and selects the post-call webhook.
func (e *ElevenLabs) SetWebhooks(ctx context.Context, inboundURL, llmSecret, postCallWebhookID string) error {
	return e.do(ctx, "PATCH", "/v1/convai/settings", map[string]any{
		"conversation_initiation_client_data_webhook": map[string]any{
			"url":             inboundURL,
			"request_headers": map[string]string{"x-orole-relay": llmSecret},
		},
		"webhooks": map[string]any{
			"post_call_webhook_id": postCallWebhookID,
			"events":               []string{"transcript", "call_initiation_failure"},
		},
	}, nil)
}

func (e *ElevenLabs) OutboundCall(ctx context.Context, c OutboundCall) (string, string, error) {
	tts := map[string]any{}
	if c.VoiceID != "" {
		tts["voice_id"] = c.VoiceID
	}
	if c.TTSModel != "" {
		tts["model_id"] = c.TTSModel
	}
	var res struct {
		Success        bool   `json:"success"`
		Message        string `json:"message"`
		ConversationID string `json:"conversation_id"`
		CallSID        string `json:"callSid"`
	}
	err := e.do(ctx, "POST", "/v1/convai/twilio/outbound-call", map[string]any{
		"agent_id": c.AgentID, "agent_phone_number_id": c.PhoneNumberID, "to_number": c.To,
		"conversation_initiation_client_data": map[string]any{
			"dynamic_variables":            map[string]string{"call_id": c.CallID, "bot_id": c.BotID},
			"conversation_config_override": map[string]any{"tts": tts},
			"custom_llm_extra_body":        map[string]string{"call_id": c.CallID},
		},
	}, &res)
	if err != nil {
		return "", "", err
	}
	if !res.Success {
		return "", "", fmt.Errorf("ElevenLabs did not start the call: %s", e.scrub(res.Message))
	}
	return res.ConversationID, res.CallSID, nil
}

func (e *ElevenLabs) ConversationStatus(ctx context.Context, id string) (string, int, error) {
	var res struct {
		Status   string `json:"status"`
		Metadata struct {
			Duration int `json:"call_duration_secs"`
		} `json:"metadata"`
	}
	err := e.do(ctx, "GET", "/v1/convai/conversations/"+url.PathEscape(id), nil, &res)
	return res.Status, res.Metadata.Duration, err
}
