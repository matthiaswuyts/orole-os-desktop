package automations

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/automations/expr"
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
	return nil
}
func (e *Engine) startWebhook(port int) error {
	webhookMu.Lock()
	defer webhookMu.Unlock()
	if e.credentials == nil {
		return fmt.Errorf("OS credential store unavailable")
	}
	secret, err := e.credentials.Get(webhookCredential)
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

// WebhookHandler verifies timestamp.body with HMAC-SHA256. The local listener
// accepts POST /automations/{id}, X-Orole-Timestamp (unix seconds), and
// X-Orole-Signature (hex HMAC). The timestamp window is 5 minutes; the signature
// also serves as a durable delivery key, so retries cannot duplicate the run.
func (e *Engine) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/automations/")
		if id == r.URL.Path || id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
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
		if err != nil || sec < now-300 || sec > now+300 {
			http.Error(w, "invalid timestamp", http.StatusUnauthorized)
			return
		}
		if e.credentials == nil {
			http.Error(w, "credential store unavailable", http.StatusServiceUnavailable)
			return
		}
		secret, err := e.credentials.Get(webhookCredential)
		if err != nil {
			http.Error(w, "credential unavailable", http.StatusServiceUnavailable)
			return
		}
		sig, err := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Orole-Signature"), "sha256="))
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(timestamp + "."))
		mac.Write(body)
		if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		d, err := e.definition(r.Context(), id)
		if err != nil || !d.Enabled || d.Trigger.Type != "webhook" {
			http.NotFound(w, r)
			return
		}
		var data any
		if err = json.Unmarshal(body, &data); err != nil {
			http.Error(w, "body must be JSON", http.StatusBadRequest)
			return
		}
		if d.Trigger.Filter != "" {
			filter, err := expr.Parse(d.Trigger.Filter)
			if err != nil {
				http.Error(w, "invalid filter", http.StatusBadRequest)
				return
			}
			matches, err := filter.EvalBool(map[string]any{"event": map[string]any{"data": data}})
			if err != nil || !matches {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		chain := []string{}
		if raw := r.Header.Get("X-Orole-Chain"); raw != "" {
			if err = json.Unmarshal([]byte(raw), &chain); err != nil {
				http.Error(w, "invalid provenance chain", http.StatusBadRequest)
				return
			}
		}
		run, err := e.trigger(context.Background(), id, "webhook", data, chain, "webhook:"+id+":"+hex.EncodeToString(sig))
		if err != nil {
			http.Error(w, e.redact.Text(err.Error()), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"runId": run.ID})
	})
}
