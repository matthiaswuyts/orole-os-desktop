package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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

func TestModelsMarksLatency(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /v1/models": `[{"model_id":"eleven_flash_v2_5","name":"Flash v2.5","can_do_text_to_speech":true},{"model_id":"eleven_multilingual_v2","name":"Multilingual v2","can_do_text_to_speech":true},{"model_id":"scribe_v1","name":"Scribe","can_do_text_to_speech":false}]`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	ms, err := el.Models(context.Background())
	if err != nil || len(ms) != 2 || ms[0].Latency != "fast" || ms[1].Latency != "expressive" {
		t.Fatalf("models=%+v err=%v", ms, err)
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
	if _, err := el.CreateAgent(context.Background(), AgentConfig{RelayURL: "https://r", LLMSecret: "ls"}); err != nil {
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
