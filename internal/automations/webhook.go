package automations

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const webhookCredential = "automations/webhook-hmac"
const MaxWebhookBody = 256 << 10

type webhookServer struct {
	server   *http.Server
	listener net.Listener
}

// Webhook reconfiguration is serialized separately from the scheduler mutex.
var webhookMu sync.Mutex

func (e *Engine) GenerateSecret() error {
	if e.credentials == nil {
		return fmt.Errorf("OS credential store unavailable")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	secret := hex.EncodeToString(b)
	if err := e.credentials.Set(webhookCredential, []byte(secret)); err != nil {
		return err
	}
	e.redact.Register(secret)
	e.setWebhookSecret([]byte(secret))
	return nil
}
func (e *Engine) startWebhook(port int) error {
	webhookMu.Lock()
	defer webhookMu.Unlock()
	if e.credentials == nil {
		return errCredentialsUnavailable
	}
	secret, err := e.webhookSecret()
	if err != nil {
		return fmt.Errorf("generate a webhook secret before enabling: %w", err)
	}
	if len(secret) < 32 {
		return fmt.Errorf("webhook secret is too short")
	}
	e.redact.Register(string(secret))
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: e.WebhookHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	e.webhook = &webhookServer{srv, ln}
	go func() { _ = srv.Serve(ln) }()
	return nil
}
func (e *Engine) stopWebhook() {
	webhookMu.Lock()
	defer webhookMu.Unlock()
	if e.webhook != nil {
		_ = e.webhook.server.Close()
		e.webhook = nil
	}
}

// WebhookHandler authenticates only the v1 envelope documented in
// docs/architecture/automations.md: method, canonical target, timestamp, exact
// provenance header and body, each prefixed by its uint64 big-endian byte length.
// The timestamp window is 5 minutes; the signature is a durable delivery key.
func (e *Engine) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/automations/")
		if id == r.URL.Path || id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
			http.NotFound(w, r)
			return
		}
		target := "/automations/" + url.PathEscape(id)
		if r.URL.EscapedPath() != target || r.URL.RawQuery != "" || r.URL.ForceQuery {
			http.NotFound(w, r)
			return
		}
		// Multiple signed headers are ambiguous across clients and proxies.
		if len(r.Header.Values("X-Orole-Timestamp")) != 1 || len(r.Header.Values("X-Orole-Signature")) != 1 || len(r.Header.Values("X-Orole-Chain")) > 1 {
			http.Error(w, "invalid authentication headers", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBody))
		if err != nil {
			http.Error(w, "body limit exceeded", http.StatusRequestEntityTooLarge)
			return
		}
		timestamp := r.Header.Get("X-Orole-Timestamp")
		sec, err := strconv.ParseInt(timestamp, 10, 64)
		now := e.clock.Now().Unix()
		if err != nil || strconv.FormatInt(sec, 10) != timestamp || sec < now-300 || sec > now+300 {
			http.Error(w, "invalid timestamp", http.StatusUnauthorized)
			return
		}
		secret, err := e.webhookSecret()
		if errors.Is(err, errCredentialsUnavailable) {
			http.Error(w, "credential store unavailable", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			http.Error(w, "credential unavailable", http.StatusServiceUnavailable)
			return
		}
		signature := r.Header.Get("X-Orole-Signature")
		if len(signature) != len("v1=")+sha256.Size*2 || !strings.HasPrefix(signature, "v1=") {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var sig [sha256.Size]byte
		if _, err := hex.Decode(sig[:], []byte(signature[len("v1="):])); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		rawChain := r.Header.Get("X-Orole-Chain")
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte("OROLE-WEBHOOK-V1\x00"))
		var size [8]byte
		for _, field := range [][]byte{[]byte(r.Method), []byte(target), []byte(timestamp), []byte(rawChain), body} {
			binary.BigEndian.PutUint64(size[:], uint64(len(field)))
			mac.Write(size[:])
			mac.Write(field)
		}
		if !hmac.Equal(sig[:], mac.Sum(nil)) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		chain := []string{}
		if rawChain != "" {
			if err := json.Unmarshal([]byte(rawChain), &chain); err != nil || chain == nil {
				http.Error(w, "invalid provenance chain", http.StatusBadRequest)
				return
			}
		}
		entry, err := e.definitionEntry(r.Context(), id)
		if err != nil || !entry.def.Enabled || entry.def.Trigger.Type != "webhook" {
			http.NotFound(w, r)
			return
		}
		d := entry.def
		var data any
		if err = json.Unmarshal(body, &data); err != nil {
			http.Error(w, "body must be JSON", http.StatusBadRequest)
			return
		}
		if d.Trigger.Filter != "" {
			if entry.filterBad {
				http.Error(w, "invalid filter", http.StatusBadRequest)
				return
			}
			matches, err := entry.filter.EvalBool(map[string]any{"event": map[string]any{"data": data}})
			if err != nil || !matches {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		run, err := e.trigger(context.Background(), id, "webhook", data, chain, "webhook:"+id+":"+hex.EncodeToString(sig[:]))
		if err != nil {
			http.Error(w, e.redact.Text(err.Error()), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"runId": run.ID})
	})
}
