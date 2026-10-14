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
