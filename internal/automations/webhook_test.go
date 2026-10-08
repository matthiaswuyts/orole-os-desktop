package automations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type webhookFixture struct {
	engine  *Engine
	client  *http.Client
	baseURL string
	dbPath  string
	secret  []byte
	stamp   string
}

func openWebhook(t *testing.T) webhookFixture {
	t.Helper()
	secret := []byte(strings.Repeat("s", 32))
	clock := &fakeClock{now: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)}
	store := &memoryCredentials{m: map[string][]byte{webhookCredential: secret}}
	dbPath := filepath.Join(t.TempDir(), "db")
	e := openEngine(t, dbPath, Options{
		Credentials: store,
		Clock:       clock,
		Notifier:    notifyFunc(func(context.Context, NotificationRequest) (any, error) { return true, nil }),
	})
	// Exercise the actual listener, HTTP parser and handler, not a recorder.
	if err := e.startWebhook(0); err != nil {
		t.Fatal(err)
	}
	addr, ok := e.webhook.listener.Addr().(*net.TCPAddr)
	if !ok || addr.IP.String() != "127.0.0.1" || addr.Port == 0 {
		t.Fatalf("webhook listener is not bound exclusively to IPv4 loopback: %v", e.webhook.listener.Addr())
	}
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return webhookFixture{
		engine: e, client: &http.Client{Transport: transport, Timeout: 5 * time.Second},
		baseURL: "http://" + addr.String(), dbPath: dbPath, secret: secret, stamp: strconv.FormatInt(clock.Now().Unix(), 10),
	}
}

func (f webhookFixture) definition(t *testing.T, id string) Definition {
	t.Helper()
	return saveDef(t, f.engine, Definition{
		ID: id, Name: "webhook", Enabled: true, Trigger: Trigger{Type: "webhook"},
		Steps: []Step{{ID: "notify", Type: "notify", Params: json.RawMessage(`{"title":"Hook","level":"info"}`)}},
	})
}

// This sender implements the public byte contract independently of the handler.
func webhookTestSignature(t *testing.T, secret []byte, method, target, stamp, chain, body string) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("OROLE-WEBHOOK-V1\x00"))
	for _, field := range []string{method, target, stamp, chain, body} {
		if err := binary.Write(mac, binary.BigEndian, uint64(len(field))); err != nil {
			t.Fatal(err)
		}
		io.WriteString(mac, field)
	}
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func (f webhookFixture) request(t *testing.T, method, target, stamp, chain, body, signature string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, f.baseURL+target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Orole-Timestamp", stamp)
	r.Header.Set("X-Orole-Signature", signature)
	if chain != "" {
		r.Header.Set("X-Orole-Chain", chain)
	}
	return r
}

func (f webhookFixture) send(t *testing.T, r *http.Request, want int) string {
	t.Helper()
	resp, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d: %s", r.Method, r.URL, resp.StatusCode, want, body)
	}
	if want != http.StatusAccepted {
		return ""
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil || accepted.RunID == "" {
		t.Fatalf("invalid accepted response %q: %v", body, err)
	}
	return accepted.RunID
}

func TestWebhookHMAC(t *testing.T) {
	f := openWebhook(t)
	d := f.definition(t, "")
	target, body := "/automations/"+d.ID, `{"value":"data"}`
	sig := webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, "", body)
	f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", body, "bad"), http.StatusUnauthorized)
	runID := f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", body, sig), http.StatusAccepted)
	awaitStatus(t, f.engine, runID, "succeeded")
	for _, replay := range []string{sig, "v1=" + strings.ToUpper(strings.TrimPrefix(sig, "v1="))} {
		if got := f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", body, replay), http.StatusAccepted); got != runID {
			t.Fatalf("replay returned run %s, want %s", got, runID)
		}
	}
	runs, err := f.engine.Runs(context.Background(), d.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("replay created %d runs: %v", len(runs), err)
	}
	// The delivery key survives engine/listener restart, not just an in-memory cache.
	f.engine.Close()
	f.engine = openEngine(t, f.dbPath, Options{
		Credentials: f.engine.credentials,
		Clock:       f.engine.clock,
		Notifier:    notifyFunc(func(context.Context, NotificationRequest) (any, error) { return true, nil }),
	})
	if err := f.engine.startWebhook(0); err != nil {
		t.Fatal(err)
	}
	f.baseURL = "http://" + f.engine.webhook.listener.Addr().String()
	if got := f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", body, sig), http.StatusAccepted); got != runID {
		t.Fatalf("replay after restart returned run %s, want %s", got, runID)
	}
}

func TestWebhookSignatureBindsTargetAndProvenance(t *testing.T) {
	f := openWebhook(t)
	a, b := f.definition(t, ""), f.definition(t, "")
	targetA, targetB := "/automations/"+a.ID, "/automations/"+b.ID
	chain, body := `["upstream"]`, `{"value":"data"}`
	sig := webhookTestSignature(t, f.secret, http.MethodPost, targetA, f.stamp, chain, body)
	runID := f.send(t, f.request(t, http.MethodPost, targetA, f.stamp, chain, body, sig), http.StatusAccepted)
	detail := awaitStatus(t, f.engine, runID, "succeeded")
	if !reflect.DeepEqual(detail.Run.Chain, []string{"upstream", a.ID}) {
		t.Fatalf("authenticated provenance lost: %v", detail.Run.Chain)
	}
	for _, tc := range []struct {
		name, target, stamp, chain, body string
	}{
		{"cross-target replay", targetB, f.stamp, chain, body},
		{"chain removed", targetA, f.stamp, "", body},
		{"chain replaced", targetA, f.stamp, `["other"]`, body},
		{"chain appended", targetA, f.stamp, `["upstream","other"]`, body},
		{"chain reserialized", targetA, f.stamp, `[ "upstream" ]`, body},
		{"body modified", targetA, f.stamp, chain, `{"value":"changed"}`},
		{"timestamp modified", targetA, strconv.FormatInt(f.engine.clock.Now().Unix()+1, 10), chain, body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.send(t, f.request(t, http.MethodPost, tc.target, tc.stamp, tc.chain, tc.body, sig), http.StatusUnauthorized)
		})
	}
	wrongMethod := webhookTestSignature(t, f.secret, http.MethodGet, targetA, f.stamp, chain, body)
	f.send(t, f.request(t, http.MethodPost, targetA, f.stamp, chain, body, wrongMethod), http.StatusUnauthorized)
	f.send(t, f.request(t, http.MethodGet, targetA, f.stamp, chain, body, sig), http.StatusMethodNotAllowed)
	for _, def := range []Definition{a, b} {
		runs, err := f.engine.Runs(context.Background(), def.ID)
		want := 0
		if def.ID == a.ID {
			want = 1
		}
		if err != nil || len(runs) != want {
			t.Fatalf("target %s has %d runs, want %d: %v", def.ID, len(runs), want, err)
		}
	}
	// The second target still accepts its own authenticated delivery.
	sigB := webhookTestSignature(t, f.secret, http.MethodPost, targetB, f.stamp, chain, body)
	runB := f.send(t, f.request(t, http.MethodPost, targetB, f.stamp, chain, body, sigB), http.StatusAccepted)
	awaitStatus(t, f.engine, runB, "succeeded")
}

func TestWebhookAuthenticatedChainEnforcement(t *testing.T) {
	f := openWebhook(t)
	d := f.definition(t, "")
	target, body := "/automations/"+d.ID, `{}`
	for _, tc := range []struct {
		name, chain string
		want        int
	}{
		{"self recursion", fmt.Sprintf("[%q]", d.ID), http.StatusConflict},
		{"depth limit", `["a","b","c"]`, http.StatusConflict},
		{"malformed JSON", `[`, http.StatusBadRequest},
		{"not an array", `{"chain":[]}`, http.StatusBadRequest},
		{"null chain", `null`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig := webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, tc.chain, body)
			f.send(t, f.request(t, http.MethodPost, target, f.stamp, tc.chain, body, sig), tc.want)
			// Removing authenticated provenance cannot bypass its enforcement.
			f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", body, sig), http.StatusUnauthorized)
		})
	}
	runs, err := f.engine.Runs(context.Background(), d.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("invalid provenance created %d runs: %v", len(runs), err)
	}
}

func TestWebhookCanonicalTarget(t *testing.T) {
	f := openWebhook(t)
	d := f.definition(t, "")
	target, body := "/automations/"+d.ID, `{}`
	sig := webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, "", body)
	for _, path := range []string{
		"/%61utomations/" + d.ID,
		"/automations/" + fmt.Sprintf("%%%02X", d.ID[0]) + d.ID[1:],
		target + "?ignored=value", target + "?", target + "/", target + "%2Fextra",
		"/automations/./" + d.ID, "/automations//" + d.ID, "/automations/.", "/automations/..",
		"/automations/" + d.ID + "%5Cextra",
	} {
		t.Run(path, func(t *testing.T) {
			f.send(t, f.request(t, http.MethodPost, path, f.stamp, "", body, sig), http.StatusNotFound)
		})
	}
	// Escaping is canonical, not a ban on IDs needing escaping. The signed
	// target is the escaped path, while dispatch looks up the decoded ID.
	escaped := f.definition(t, "target space")
	escapedPath := "/automations/target%20space"
	escapedSig := webhookTestSignature(t, f.secret, http.MethodPost, escapedPath, f.stamp, "", body)
	runID := f.send(t, f.request(t, http.MethodPost, escapedPath, f.stamp, "", body, escapedSig), http.StatusAccepted)
	detail := awaitStatus(t, f.engine, runID, "succeeded")
	if detail.Run.AutomationID != escaped.ID {
		t.Fatalf("escaped target dispatched to %q", detail.Run.AutomationID)
	}
	f.send(t, f.request(t, http.MethodPost, "/automations/target%2520space", f.stamp, "", body, escapedSig), http.StatusUnauthorized)
}

func TestWebhookRejectsLegacyMalformedAndAmbiguousAuthentication(t *testing.T) {
	f := openWebhook(t)
	d := f.definition(t, "")
	target, body := "/automations/"+d.ID, `{}`
	sig := webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, "", body)
	legacy := hmac.New(sha256.New, f.secret)
	io.WriteString(legacy, f.stamp+"."+body)
	legacyHex := hex.EncodeToString(legacy.Sum(nil))
	for _, signature := range []string{
		"", "bad", legacyHex, "sha256=" + legacyHex, "v1=" + legacyHex,
		"v2=" + sig[3:], "v1=" + strings.Repeat("z", 64), sig[:len(sig)-1], sig + "00", sig + "," + sig,
	} {
		t.Run(signature, func(t *testing.T) {
			f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", body, signature), http.StatusUnauthorized)
		})
	}
	for _, header := range []string{"X-Orole-Timestamp", "X-Orole-Signature", "X-Orole-Chain"} {
		t.Run("duplicate "+header, func(t *testing.T) {
			chain := `[]`
			signed := webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, chain, body)
			r := f.request(t, http.MethodPost, target, f.stamp, chain, body, signed)
			r.Header.Add(header, r.Header.Get(header))
			f.send(t, r, http.StatusUnauthorized)
		})
	}
	for _, header := range []string{"X-Orole-Timestamp", "X-Orole-Signature"} {
		t.Run("missing "+header, func(t *testing.T) {
			r := f.request(t, http.MethodPost, target, f.stamp, "", body, sig)
			r.Header.Del(header)
			f.send(t, r, http.StatusUnauthorized)
		})
	}
	runs, err := f.engine.Runs(context.Background(), d.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("malformed authentication created %d runs: %v", len(runs), err)
	}
}

func TestWebhookTimestampWindowAndBodyLimits(t *testing.T) {
	f := openWebhook(t)
	d := f.definition(t, "")
	target, body := "/automations/"+d.ID, `{}`
	for _, delta := range []int64{-301, -300, 300, 301} {
		stamp := strconv.FormatInt(f.engine.clock.Now().Unix()+delta, 10)
		sig := webhookTestSignature(t, f.secret, http.MethodPost, target, stamp, "", body)
		want := http.StatusAccepted
		if delta < -300 || delta > 300 {
			want = http.StatusUnauthorized
		}
		runID := f.send(t, f.request(t, http.MethodPost, target, stamp, "", body, sig), want)
		if want == http.StatusAccepted {
			awaitStatus(t, f.engine, runID, "succeeded")
		}
	}
	for _, stamp := range []string{"+" + f.stamp, "0" + f.stamp, "9223372036854775807", "-9223372036854775808", "not-a-time"} {
		sig := webhookTestSignature(t, f.secret, http.MethodPost, target, stamp, "", body)
		f.send(t, f.request(t, http.MethodPost, target, stamp, "", body, sig), http.StatusUnauthorized)
	}
	oversized := strings.Repeat("x", MaxWebhookBody+1)
	sig := webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, "", oversized)
	f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", oversized, sig), http.StatusRequestEntityTooLarge)
	invalidJSON := "{"
	sig = webhookTestSignature(t, f.secret, http.MethodPost, target, f.stamp, "", invalidJSON)
	f.send(t, f.request(t, http.MethodPost, target, f.stamp, "", invalidJSON, sig), http.StatusBadRequest)
}
