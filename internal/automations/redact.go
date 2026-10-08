package automations

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
)

// Redactor stores known secret values in memory only. Register every secret when
// loading it from Credential Manager; journals and audit text use the same scrubber.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

var bearerRE = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]+`)
var tokenRE = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,}|sk-[A-Za-z0-9_-]{16,})\b`)

func (r *Redactor) Register(secret string) {
	if secret == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.secrets {
		if s == secret {
			return
		}
	}
	r.secrets = append(r.secrets, secret)
}
func (r *Redactor) Text(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.secrets {
		s = strings.ReplaceAll(s, v, "[REDACTED]")
	}
	s = bearerRE.ReplaceAllString(s, "Bearer [REDACTED]")
	return tokenRE.ReplaceAllString(s, "[REDACTED]")
}
func (r *Redactor) JSON(b []byte) json.RawMessage {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return json.RawMessage(`null`)
	}
	v = r.value(v)
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	if len(out) > 128<<10 {
		return json.RawMessage(`{"truncated":true,"message":"Output exceeds journal limit"}`)
	}
	return out
}
func (r *Redactor) value(v any) any {
	switch x := v.(type) {
	case string:
		return r.Text(x)
	case []any:
		for i := range x {
			x[i] = r.value(x[i])
		}
	case map[string]any:
		for k, v := range x {
			switch strings.ToLower(k) {
			case "authorization", "password", "secret", "token", "access_token", "refresh_token", "api_key", "apikey":
				x[k] = "[REDACTED]"
			default:
				x[k] = r.value(v)
			}
		}
	}
	return v
}
