package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/jarvis"
)

var JarvisChanged = mygo.NewEvent[jarvis.StatusView]("jarvis:changed")

func forwardJarvis(event string, data json.RawMessage) bool {
	if event != "jarvis.changed" {
		return false
	}
	var v jarvis.StatusView
	if json.Unmarshal(data, &v) == nil {
		JarvisChanged.Broadcast(v)
	}
	return true
}

// Jarvis configures phone calls with bots. Secrets go straight to the daemon's Credential Manager and are never read back.
type Jarvis struct{ L *Link }

func (s Jarvis) Status(ctx context.Context) (jarvis.StatusView, error) {
	var out jarvis.StatusView
	return out, s.L.call(ctx, "jarvis.status", nil, &out)
}

func (s Jarvis) SetSecrets(ctx context.Context, in jarvis.SecretsInput) (jarvis.StatusView, error) {
	var out jarvis.StatusView
	return out, s.L.call(ctx, "jarvis.secrets.set", in, &out)
}

func (s Jarvis) GenerateRelaySecrets(ctx context.Context) (jarvis.RelaySecrets, error) {
	var out jarvis.RelaySecrets
	return out, s.L.call(ctx, "jarvis.relaySecrets.generate", nil, &out)
}

// Configure saves the phone settings and provisions the ElevenLabs agent. The result carries the post-call
// webhook secret once, on the run that created the webhook.
func (s Jarvis) Configure(ctx context.Context, in jarvis.PhoneSettings) (jarvis.ConfigureResult, error) {
	var out jarvis.ConfigureResult
	return out, s.L.call(ctx, "jarvis.configure", in, &out)
}

func (s Jarvis) StartVerification(ctx context.Context, number string) error {
	return s.L.call(ctx, "jarvis.verify.start", map[string]string{"number": number}, nil)
}

func (s Jarvis) ConfirmVerification(ctx context.Context, code string) (jarvis.StatusView, error) {
	var out jarvis.StatusView
	return out, s.L.call(ctx, "jarvis.verify.confirm", map[string]string{"code": code}, &out)
}

func (s Jarvis) Voices(ctx context.Context) ([]jarvis.Voice, error) {
	var out []jarvis.Voice
	return out, s.L.call(ctx, "jarvis.voices", nil, &out)
}

func (s Jarvis) Models(ctx context.Context) ([]jarvis.TTSModel, error) {
	var out []jarvis.TTSModel
	return out, s.L.call(ctx, "jarvis.models", nil, &out)
}

func (s Jarvis) Rules(ctx context.Context, botID string) ([]jarvis.JarvisRule, error) {
	var out []jarvis.JarvisRule
	return out, s.L.call(ctx, "jarvis.rules.list", map[string]string{"botId": botID}, &out)
}

func (s Jarvis) DeleteRule(ctx context.Context, id string) error {
	return s.L.call(ctx, "jarvis.rules.delete", map[string]string{"id": id}, nil)
}

// Transcript returns a phone call and its spoken turns, which the chat timeline does not show.
func (s Jarvis) Transcript(ctx context.Context, callID string) (jarvis.CallTranscript, error) {
	var out jarvis.CallTranscript
	return out, s.L.call(ctx, "jarvis.calls.transcript", map[string]string{"callId": callID}, &out)
}

func (s Jarvis) TestCall(ctx context.Context, botID string) (string, error) {
	var out string
	return out, s.L.call(ctx, "jarvis.call.test", map[string]string{"botId": botID}, &out)
}
