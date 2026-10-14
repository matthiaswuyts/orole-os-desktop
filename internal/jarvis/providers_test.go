package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorded struct {
	Method, Path string
	Body         map[string]any
	Header       http.Header
}

func fakeAPI(t *testing.T, replies map[string]string) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		got = append(got, recorded{r.Method, r.URL.Path, body, r.Header.Clone()})
		mu.Unlock()
		key := r.Method + " " + r.URL.Path
		reply, ok := replies[key]
		if !ok {
			http.Error(w, `{"detail":"unexpected `+key+`"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recorded { mu.Lock(); defer mu.Unlock(); return append([]recorded(nil), got...) }
}

func TestElevenLabsOutboundCallSendsVariablesAndVoice(t *testing.T) {
	srv, reqs := fakeAPI(t, map[string]string{"POST /v1/convai/twilio/outbound-call": `{"success":true,"conversation_id":"conv1","callSid":"CA1"}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	conv, sid, err := el.OutboundCall(context.Background(), OutboundCall{AgentID: "ag", PhoneNumberID: "pn", To: "+32470000000", CallID: "c1", BotID: "b1", VoiceID: "v1", TTSModel: "eleven_flash_v2_5"})
	if err != nil || conv != "conv1" || sid != "CA1" {
		t.Fatalf("conv=%q sid=%q err=%v", conv, sid, err)
	}
	r := reqs()[0]
	if r.Header.Get("xi-api-key") != "k" {
		t.Error("api key header missing")
	}
	b, _ := json.Marshal(r.Body)
	for _, want := range []string{`"to_number":"+32470000000"`, `"agent_phone_number_id":"pn"`, `"call_id":"c1"`, `"bot_id":"b1"`, `"custom_llm_extra_body":{"call_id":"c1"}`, `"voice_id":"v1"`, `"model_id":"eleven_flash_v2_5"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("body lacks %s: %s", want, b)
		}
	}
}

func TestElevenLabsErrorsNeverEchoKey(t *testing.T) {
	srv, _ := fakeAPI(t, nil)
	el := &ElevenLabs{Key: "sekret-key", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := el.Voices(context.Background())
	if err == nil || strings.Contains(err.Error(), "sekret-key") {
		t.Fatalf("err=%v", err)
	}
}

func TestModelsAreCuratedFilteredAndOrdered(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /v1/models": `[{"model_id":"eleven_turbo_v2_5","name":"x","can_do_text_to_speech":true},{"model_id":"eleven_multilingual_v2","name":"Multilingual v2","can_do_text_to_speech":true},{"model_id":"eleven_flash_v2_5","name":"y","can_do_text_to_speech":true},{"model_id":"eleven_v3_conversational","name":"z","can_do_text_to_speech":false},{"model_id":"scribe_v1","name":"Scribe","can_do_text_to_speech":false}]`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	ms, err := el.Models(context.Background())
	if err != nil || len(ms) != 2 {
		t.Fatalf("models=%+v err=%v", ms, err)
	}
	if ms[0].ID != "eleven_flash_v2_5" || !ms[0].Recommended || ms[0].Name != "Flash v2.5" || ms[0].Description == "" {
		t.Errorf("first=%+v", ms[0])
	}
	if ms[1].ID != "eleven_turbo_v2_5" || ms[1].Recommended || ms[1].Name != "Turbo v2.5 (older)" {
		t.Errorf("second=%+v", ms[1])
	}
}

func TestModelsHidesUnknownAccountModels(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /v1/models": `[{"model_id":"eleven_multilingual_v2","name":"Multilingual v2","can_do_text_to_speech":true}]`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	ms, err := el.Models(context.Background())
	if err != nil || len(ms) != 0 || ms == nil {
		t.Fatalf("models=%+v err=%v", ms, err)
	}
}

func TestVoicesDescribeThemselves(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /v1/voices": `{"voices":[
{"voice_id":"v1","name":"Rachel","description":"  Warm and clear.  ","labels":{"gender":"female"}},
{"voice_id":"v2","name":"George","description":"","labels":{"use_case":"narration","accent":"British","gender":"male","descriptive":"calm","age":""}},
{"voice_id":"v3","name":"Mystery","labels":{}}]}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	vs, err := el.Voices(context.Background())
	if err != nil || len(vs) != 3 {
		t.Fatalf("voices=%+v err=%v", vs, err)
	}
	if vs[0].Description != "Warm and clear." {
		t.Errorf("v1=%q", vs[0].Description)
	}
	if vs[1].Description != "Calm · male · British · narration" {
		t.Errorf("v2=%q", vs[1].Description)
	}
	if vs[2].Description != "" {
		t.Errorf("v3=%q", vs[2].Description)
	}
}

func TestConversationStatus(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /v1/convai/conversations/c9": `{"status":"done","metadata":{"call_duration_secs":42}}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	st, d, err := el.ConversationStatus(context.Background(), "c9")
	if err != nil || st != "done" || d != 42 {
		t.Fatalf("%q %d %v", st, d, err)
	}
}

func provisionFake(t *testing.T, extra map[string]string) (*httptest.Server, func() []recorded) {
	r := map[string]string{
		"POST /v1/convai/agents/create":                          `{"agent_id":"ag1"}`,
		"POST /v1/convai/phone-numbers":                          `{"phone_number_id":"pn1"}`,
		"PATCH /v1/convai/phone-numbers/pn1":                     `{}`,
		"POST /v1/workspace/webhooks":                            `{"webhook_id":"wh1","webhook_secret":"whsec"}`,
		"PATCH /v1/convai/settings":                              `{}`,
		"GET /2010-04-01/Accounts/AC1/IncomingPhoneNumbers.json": `{"incoming_phone_numbers":[{"phone_number":"+3220000000"}]}`,
	}
	for k, v := range extra {
		r[k] = v
	}
	return fakeAPI(t, r)
}

func TestProvisionCreatesAgentAndImportsNumber(t *testing.T) {
	srv, reqs := provisionFake(t, nil)
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	var stored string
	s, err := Provision(context.Background(), el, tw, PhoneSettings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000"},
		ProvisionSecrets{LLMSecret: "ls", TwilioSID: "AC1", TwilioToken: "t", StoreWebhookSecret: func(v string) error { stored = v; return nil }})
	if err != nil || s.AgentID != "ag1" || s.PhoneNumberID != "pn1" || s.PostCallWebhookID != "wh1" {
		t.Fatalf("s=%+v err=%v", s, err)
	}
	if stored != "whsec" {
		t.Errorf("webhook secret not handed to caller: %q", stored)
	}
	var create, hook, settings recorded
	for _, r := range reqs() {
		switch r.Path {
		case "/v1/convai/agents/create":
			create = r
		case "/v1/workspace/webhooks":
			hook = r
		case "/v1/convai/settings":
			settings = r
		}
	}
	b, _ := json.Marshal(create.Body)
	for _, want := range []string{`"url":"https://relay.example/llm"`, `"x-orole-relay":"ls"`, `"first_message":""`, `"built_in_tools"`, `"voicemail_detection"`, `"custom_llm_extra_body":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("agent config lacks %s: %s", want, b)
		}
	}
	if strings.Contains(strings.ToLower(string(b)), "orole os") {
		t.Error("agent config names the app")
	}
	hb, _ := json.Marshal(hook.Body)
	for _, want := range []string{`"auth_type":"hmac"`, `"webhook_url":"https://relay.example/status"`} {
		if !strings.Contains(string(hb), want) {
			t.Errorf("webhook lacks %s: %s", want, hb)
		}
	}
	sb, _ := json.Marshal(settings.Body)
	for _, want := range []string{`"url":"https://relay.example/inbound"`, `"x-orole-relay":"ls"`, `"post_call_webhook_id":"wh1"`} {
		if !strings.Contains(string(sb), want) {
			t.Errorf("settings lacks %s: %s", want, sb)
		}
	}
}

func TestProvisionIsIdempotent(t *testing.T) {
	srv, reqs := provisionFake(t, map[string]string{"PATCH /v1/convai/agents/ag1": `{}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	in := PhoneSettings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000", AgentID: "ag1", PhoneNumberID: "pn1", PostCallWebhookID: "wh1"}
	if _, err := Provision(context.Background(), el, tw, in, ProvisionSecrets{LLMSecret: "ls"}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range reqs() {
		seen[r.Method+" "+r.Path] = true
	}
	if seen["POST /v1/convai/agents/create"] || seen["POST /v1/convai/phone-numbers"] || seen["POST /v1/workspace/webhooks"] {
		t.Errorf("recreated existing resources: %v", seen)
	}
	if !seen["PATCH /v1/convai/agents/ag1"] {
		t.Errorf("did not update agent: %v", seen)
	}
}

func TestProvisionRejectsNumberNotInTwilio(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /2010-04-01/Accounts/AC1/IncomingPhoneNumbers.json": `{"incoming_phone_numbers":[]}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := Provision(context.Background(), el, tw, PhoneSettings{RelayURL: "https://r", TwilioNumber: "+1"}, ProvisionSecrets{}); err == nil {
		t.Fatal("accepted a number Twilio does not own")
	}
}

func TestTwilioBadCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	tw := &Twilio{SID: "AC1", Token: "tok-secret", BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := tw.Numbers(context.Background())
	if err == nil || strings.Contains(err.Error(), "tok-secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestProvisionKeepsWebhookIDUnsetWhenSecretNotStored(t *testing.T) {
	for name, store := range map[string]func(string) error{
		"store fails": func(string) error { return errors.New("vault down") },
		"no callback": nil,
	} {
		srv, _ := provisionFake(t, nil)
		el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
		tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
		s, err := Provision(context.Background(), el, tw, PhoneSettings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000"},
			ProvisionSecrets{LLMSecret: "ls", TwilioSID: "AC1", TwilioToken: "t", StoreWebhookSecret: store})
		if err == nil || s.PostCallWebhookID != "" {
			t.Errorf("%s: id=%q err=%v", name, s.PostCallWebhookID, err)
		}
	}
}

func TestAgentBodyOmitsEmptyVoiceAndModel(t *testing.T) {
	srv, reqs := provisionFake(t, nil)
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	// English: ElevenLabs' default model is fine, so nothing is sent.
	if _, err := el.CreateAgent(context.Background(), AgentConfig{RelayURL: "https://r", LLMSecret: "ls", Language: LangEnglish}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(reqs()[0].Body)
	conv := reqs()[0].Body["conversation_config"].(map[string]any)
	if _, ok := conv["tts"]; ok {
		t.Errorf("tts present with no voice/model: %s", b)
	}
	if strings.Contains(string(b), `"voice_id":""`) || strings.Contains(string(b), `"model_id":""`) {
		t.Errorf("empty ids sent: %s", b)
	}
}

func TestProvisionScrubsLLMSecretFromErrors(t *testing.T) {
	good, _ := provisionFake(t, nil)
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: good.URL, HTTP: good.Client()}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad header supersecretllm", 400)
	}))
	defer bad.Close()
	el := &ElevenLabs{Key: "k", BaseURL: bad.URL, HTTP: bad.Client()}
	_, err := Provision(context.Background(), el, tw, PhoneSettings{RelayURL: "https://r", TwilioNumber: "+3220000000"}, ProvisionSecrets{LLMSecret: "supersecretllm"})
	if err == nil || strings.Contains(err.Error(), "supersecretllm") {
		t.Fatalf("err=%v", err)
	}
}

func TestTwilioHangupPostsCompleted(t *testing.T) {
	var got struct {
		method, path, ctype, status, user, pass string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got.method, got.path, got.ctype, got.status = r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.PostForm.Get("Status")
		got.user, got.pass, _ = r.BasicAuth()
		_, _ = io.WriteString(w, `{"status":"completed"}`)
	}))
	defer srv.Close()
	tw := &Twilio{SID: "AC1", Token: "tok-secret", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := tw.Hangup(context.Background(), testSID); err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/2010-04-01/Accounts/AC1/Calls/"+testSID+".json" || got.status != "completed" ||
		got.ctype != "application/x-www-form-urlencoded" || got.user != "AC1" || got.pass != "tok-secret" {
		t.Fatalf("request %+v", got)
	}
}

func TestTwilioCallStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/2010-04-01/Accounts/AC1/Calls/"+testSID+".json" {
			w.WriteHeader(404)
			return
		}
		_, _ = io.WriteString(w, `{"sid":"`+testSID+`","status":"no-answer"}`)
	}))
	defer srv.Close()
	tw := &Twilio{SID: "AC1", Token: "tok", BaseURL: srv.URL, HTTP: srv.Client()}
	st, err := tw.CallStatus(context.Background(), testSID)
	if err != nil || st != "no-answer" {
		t.Fatalf("status=%q err=%v", st, err)
	}
}

func TestTwilioCallErrorsNeverEchoToken(t *testing.T) {
	for _, code := range []int{401, 404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"message":"bad tok-secret"}`)
		}))
		tw := &Twilio{SID: "AC1", Token: "tok-secret", BaseURL: srv.URL, HTTP: srv.Client()}
		err1 := tw.Hangup(context.Background(), testSID)
		_, err2 := tw.CallStatus(context.Background(), testSID)
		srv.Close()
		for _, err := range []error{err1, err2} {
			if err == nil || strings.Contains(err.Error(), "tok-secret") || strings.Contains(err.Error(), srv.URL) {
				t.Fatalf("code %d: err=%v", code, err)
			}
		}
	}
}

func TestTwilioRejectsBadCallSID(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	tw := &Twilio{SID: "AC1", Token: "tok", BaseURL: srv.URL, HTTP: srv.Client()}
	for _, sid := range []string{"", "CA1", "../IncomingPhoneNumbers", "CA0123456789abcdef0123456789abcdef/x"} {
		if err := tw.Hangup(context.Background(), sid); err == nil {
			t.Errorf("accepted %q", sid)
		}
	}
	if hits != 0 {
		t.Fatalf("%d requests sent", hits)
	}
}

func TestAgentBodyLanguage(t *testing.T) {
	type agent struct {
		ConversationConfig struct {
			Agent struct {
				Language string `json:"language"`
				Prompt   struct {
					BuiltInTools map[string]json.RawMessage `json:"built_in_tools"`
				} `json:"prompt"`
			} `json:"agent"`
			LanguagePresets map[string]json.RawMessage `json:"language_presets"`
		} `json:"conversation_config"`
	}
	for _, c := range []struct {
		lang, want string
		detect     bool
	}{{"", "nl", false}, {LangDutch, "nl", false}, {LangEnglish, "en", false}, {LangAuto, "nl", true}} {
		raw, err := json.Marshal(agentBody(AgentConfig{RelayURL: "https://r", LLMSecret: "s", Language: c.lang}))
		if err != nil {
			t.Fatal(err)
		}
		var a agent
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		cc := a.ConversationConfig
		if cc.Agent.Language != c.want {
			t.Errorf("%q: language %q, want %q", c.lang, cc.Agent.Language, c.want)
		}
		tool := string(cc.Agent.Prompt.BuiltInTools["language_detection"])
		if got := strings.Contains(tool, `"system_tool_type":"language_detection"`); got != c.detect {
			t.Errorf("%q: language_detection tool = %s", c.lang, tool)
		}
		if !c.detect && tool != "null" {
			t.Errorf("%q: language_detection must be cleared, got %s", c.lang, tool)
		}
		if _, ok := cc.LanguagePresets["en"]; ok != c.detect {
			t.Errorf("%q: language presets %v", c.lang, cc.LanguagePresets)
		}
	}
}

func TestProvisionSendsCallLanguage(t *testing.T) {
	if got := (PhoneSettings{}).callLanguage(); got != LangDutch {
		t.Fatalf("default call language %q", got)
	}
	if _, err := validateSettings(PhoneSettings{CallLanguage: "fr"}); err == nil {
		t.Fatal("accepted an unknown call language")
	}
	for _, l := range []string{"", LangDutch, LangEnglish, LangAuto} {
		if _, err := validateSettings(PhoneSettings{CallLanguage: l}); err != nil {
			t.Fatalf("%q: %v", l, err)
		}
	}
}

// Changing the call language re-provisions: Configure runs Provision, which updates the existing agent with it.
func TestProvisionUpdatesAgentLanguage(t *testing.T) {
	srv, reqs := provisionFake(t, map[string]string{"PATCH /v1/convai/agents/ag1": `{}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	in := PhoneSettings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000", AgentID: "ag1", PhoneNumberID: "pn1", PostCallWebhookID: "wh1", CallLanguage: LangEnglish}
	if _, err := Provision(context.Background(), el, tw, in, ProvisionSecrets{LLMSecret: "ls"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range reqs() {
		if r.Method+" "+r.Path == "PATCH /v1/convai/agents/ag1" {
			b, _ := json.Marshal(r.Body)
			if !strings.Contains(string(b), `"language":"en"`) {
				t.Fatalf("agent update lacks the language: %s", b)
			}
			return
		}
	}
	t.Fatal("agent not updated")
}

// A non-English agent needs a multilingual voice model; with none chosen, Flash v2.5 is sent (create, update and per call).
func TestNonEnglishCallsGetMultilingualModel(t *testing.T) {
	for _, lang := range []string{"", LangDutch, LangAuto} {
		raw, _ := json.Marshal(agentBody(AgentConfig{RelayURL: "https://r", LLMSecret: "s", Language: lang}))
		if !strings.Contains(string(raw), `"tts":{"model_id":"eleven_flash_v2_5"}`) {
			t.Errorf("%q: agent body lacks the multilingual model: %s", lang, raw)
		}
	}
	raw, _ := json.Marshal(agentBody(AgentConfig{RelayURL: "https://r", LLMSecret: "s", Language: LangDutch, TTSModel: "eleven_v3_conversational"}))
	if !strings.Contains(string(raw), `"model_id":"eleven_v3_conversational"`) {
		t.Errorf("chosen model replaced: %s", raw)
	}
	if ttsModel(LangEnglish, "") != "" || ttsModel(LangDutch, "") != multilingualModel || ttsModel(LangAuto, "x") != "x" {
		t.Error("ttsModel")
	}
}

// jsonPath reads a nested value out of a decoded JSON document ("conversation_config", "turn", ...).
func jsonPath(t *testing.T, doc any, path ...string) any {
	t.Helper()
	for _, k := range path {
		m, ok := doc.(map[string]any)
		if !ok {
			t.Fatalf("%v: not an object at %q", path, k)
		}
		if doc, ok = m[k]; !ok {
			t.Fatalf("%v: missing %q", path, k)
		}
	}
	return doc
}

// The agent is patient (no cut-offs on hesitations), waits for a slow custom LLM, never ends a call on silence, ends at
// the daemon's ceiling and offers end_call and skip_turn to the LLM; everything it sent before stays.
func TestAgentBodyTurnTakingAndTimeouts(t *testing.T) {
	for _, lang := range []string{"", LangDutch, LangEnglish, LangAuto} {
		raw, err := json.Marshal(agentBody(AgentConfig{RelayURL: "https://r/", LLMSecret: "s", Language: lang, VoiceID: "v1"}))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		cfg := func(path ...string) any { return jsonPath(t, doc, append([]string{"conversation_config"}, path...)...) }
		for _, c := range []struct {
			path []string
			want any
		}{
			{[]string{"turn", "turn_eagerness"}, "patient"},
			{[]string{"turn", "turn_timeout"}, 10.0},
			{[]string{"turn", "initial_wait_time"}, 1.0},
			{[]string{"turn", "speculative_turn"}, false},
			{[]string{"turn", "silence_end_call_timeout"}, -1.0},
			{[]string{"turn", "soft_timeout_config", "timeout_seconds"}, -1.0},
			{[]string{"conversation", "max_duration_seconds"}, 1800.0},
			{[]string{"agent", "prompt", "cascade_timeout_seconds"}, 15.0},
			{[]string{"agent", "prompt", "built_in_tools", "skip_turn", "name"}, "skip_turn"},
			{[]string{"agent", "prompt", "built_in_tools", "skip_turn", "params", "system_tool_type"}, "skip_turn"},
			// unchanged
			{[]string{"agent", "prompt", "built_in_tools", "end_call", "params", "system_tool_type"}, "end_call"},
			{[]string{"agent", "prompt", "built_in_tools", "voicemail_detection", "params", "system_tool_type"}, "voicemail_detection"},
			{[]string{"agent", "prompt", "llm"}, "custom-llm"},
			{[]string{"agent", "prompt", "custom_llm", "url"}, "https://r/llm"},
			{[]string{"agent", "prompt", "custom_llm", "request_headers", "x-orole-relay"}, "s"},
			{[]string{"agent", "first_message"}, ""},
			{[]string{"tts", "voice_id"}, "v1"},
		} {
			if got := cfg(c.path...); got != c.want {
				t.Errorf("%q: %s = %v, want %v", lang, strings.Join(c.path, "."), got, c.want)
			}
		}
		if got := jsonPath(t, doc, "platform_settings", "overrides", "custom_llm_extra_body"); got != true {
			t.Errorf("%q: custom_llm_extra_body override = %v", lang, got)
		}
	}
	if agentRevision < 2 {
		t.Errorf("agentRevision %d: the turn-taking change needs revision 2 or later", agentRevision)
	}
}

func TestProvisionRecordsAgentRevision(t *testing.T) {
	srv, _ := provisionFake(t, map[string]string{"PATCH /v1/convai/agents/ag1": `{}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	base := PhoneSettings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000", CallLanguage: LangDutch}
	// Create path (no agent yet), then update path (an agent recorded with an older revision).
	created, err := Provision(context.Background(), el, tw, base, ProvisionSecrets{LLMSecret: "ls", StoreWebhookSecret: func(string) error { return nil }})
	if err != nil || created.AgentRevision != agentRevision || created.AgentLanguage != LangDutch {
		t.Fatalf("created: %+v err=%v", created, err)
	}
	old := created
	old.AgentRevision = 0
	updated, err := Provision(context.Background(), el, tw, old, ProvisionSecrets{LLMSecret: "ls"})
	if err != nil || updated.AgentRevision != agentRevision {
		t.Fatalf("updated: %+v err=%v", updated, err)
	}
}

func TestConversationOutcome(t *testing.T) {
	failed := `{"agent_id":"ag","conversation_id":"c1","status":"failed","transcript":[{"role":"user","message":"never read"}],
"metadata":{"start_time_unix_secs":1,"call_duration_secs":9,"termination_reason":"custom_llm generation failed",
"error":{"code":1002,"error_type":"llm_error","reason":"custom_llm_error: LLM Cascade Error: TimeoutError"},
"phone_call":{"direction":"outbound","call_sid":"CA1"}}}`
	srv, _ := fakeAPI(t, map[string]string{
		"GET /v1/convai/conversations/c1": failed,
		"GET /v1/convai/conversations/c2": `{"status":"done","metadata":{"termination_reason":"end_call tool was called.","error":null,"phone_call":null}}`,
		"GET /v1/convai/conversations/c3": `{"status":"failed","metadata":{"error":"plain text with sekret-relay"}}`,
		"GET /v1/convai/conversations/c4": `{"status":"processing"}`,
	})
	el := &ElevenLabs{Key: "xi-test-key", BaseURL: srv.URL, HTTP: srv.Client(), Scrub: []string{"sekret-relay"}}
	for _, c := range []struct {
		id   string
		want Outcome
	}{
		{"c1", Outcome{Status: "failed", TerminationReason: "custom_llm generation failed", ErrorType: "llm_error", ErrorReason: "custom_llm_error: LLM Cascade Error: TimeoutError"}},
		{"c2", Outcome{Status: "done", TerminationReason: "end_call tool was called."}},
		{"c3", Outcome{Status: "failed", ErrorReason: "plain text with [redacted]"}},
		{"c4", Outcome{Status: "processing"}},
	} {
		got, err := el.ConversationOutcome(context.Background(), c.id)
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v err=%v, want %+v", c.id, got, err, c.want)
		}
	}
	if _, err := el.ConversationOutcome(context.Background(), "missing"); err == nil {
		t.Error("an unknown conversation was not an error")
	}
}

func TestFindConversationMatchesByCallSID(t *testing.T) {
	near := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) int64 { return near.Add(d).Unix() }
	var mu sync.Mutex
	var listQuery url.Values
	var details []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/convai/conversations":
			listQuery = r.URL.Query()
			// Newest first, as the real list is. conv-far has the right SID but started ten minutes away.
			_, _ = fmt.Fprintf(w, `{"conversations":[
{"conversation_id":"conv-new","start_time_unix_secs":%d,"status":"done"},
{"conversation_id":"conv-hit","start_time_unix_secs":%d,"status":"failed"},
{"conversation_id":"conv-other","start_time_unix_secs":%d,"status":"done"},
{"conversation_id":"conv-far","start_time_unix_secs":%d,"status":"done"}],"has_more":false}`,
				at(90*time.Second), at(-20*time.Second), at(-100*time.Second), at(-10*time.Minute))
		case strings.HasPrefix(r.URL.Path, "/v1/convai/conversations/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/convai/conversations/")
			details = append(details, id)
			sid := map[string]string{"conv-new": "CAnew", "conv-hit": "CAwanted", "conv-other": "CAother", "conv-far": "CAwanted"}[id]
			_, _ = fmt.Fprintf(w, `{"conversation_id":%q,"status":"done","metadata":{"phone_call":{"direction":"inbound","call_sid":%q}}}`, id, sid)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}

	id, err := el.FindConversation(context.Background(), "ag1", "CAwanted", near)
	if err != nil || id != "conv-hit" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	mu.Lock()
	if listQuery.Get("agent_id") != "ag1" || listQuery.Get("page_size") != "10" {
		t.Errorf("list query %v", listQuery)
	}
	if !slices.Equal(details, []string{"conv-new", "conv-hit"}) {
		t.Errorf("details fetched for %v: the far conversation must be skipped and the search must stop at the match", details)
	}
	details = nil
	mu.Unlock()

	// No call in the window has that SID: nothing found, no error. The far conversation is not a candidate.
	id, err = el.FindConversation(context.Background(), "ag1", "CAnobody", near)
	if err != nil || id != "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if id, err = el.FindConversation(context.Background(), "ag1", "CAwanted", near.Add(time.Hour)); err != nil || id != "" {
		t.Fatalf("a window an hour later matched %q err=%v", id, err)
	}
	if _, err = el.FindConversation(context.Background(), "", "CAwanted", near); err == nil {
		t.Error("an empty agent id was accepted")
	}
	if _, err = el.FindConversation(context.Background(), "ag1", "", near); err == nil {
		t.Error("an empty call SID was accepted")
	}
}

func TestFindConversationReportsErrors(t *testing.T) {
	near := time.Now()
	srv, _ := fakeAPI(t, nil) // everything 404s
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	if id, err := el.FindConversation(context.Background(), "ag1", "CA1", near); err == nil || id != "" {
		t.Fatalf("list failure: id=%q err=%v", id, err)
	}
	// The list works but the detail of the only candidate fails, and nothing else matches: that is an error, not "none".
	srv2, _ := fakeAPI(t, map[string]string{"GET /v1/convai/conversations": fmt.Sprintf(`{"conversations":[{"conversation_id":"c1","start_time_unix_secs":%d}]}`, near.Unix())})
	el2 := &ElevenLabs{Key: "k", BaseURL: srv2.URL, HTTP: srv2.Client()}
	if id, err := el2.FindConversation(context.Background(), "ag1", "CA1", near); err == nil || id != "" {
		t.Fatalf("detail failure: id=%q err=%v", id, err)
	}
}
