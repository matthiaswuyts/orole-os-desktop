package jarvis

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// ProvisionSecrets carries credentials into Provision. ElevenLabs generates the post-call webhook HMAC secret
// itself and returns it only on creation, so StoreWebhookSecret is called with it (when a webhook is created)
// for the caller to persist under KeyWebhookSecret and deploy to the relay.
type ProvisionSecrets struct {
	LLMSecret, TwilioSID, TwilioToken string
	StoreWebhookSecret                func(secret string) error
}

// Provision makes the ElevenLabs side match the settings: one agent pointing at the relay, the Twilio number
// imported and assigned to it, a post-call webhook, and the workspace inbound webhook pointing at the relay.
// Safe to run repeatedly; existing agent, number and webhook ids in s are reused.
func Provision(ctx context.Context, el *ElevenLabs, tw *Twilio, s Settings, sec ProvisionSecrets) (Settings, error) {
	nums, err := tw.Numbers(ctx)
	if err != nil {
		return s, err
	}
	if !slices.Contains(nums, s.TwilioNumber) {
		return s, fmt.Errorf("The number %s is not in this Twilio account.", s.TwilioNumber)
	}
	base := strings.TrimRight(s.RelayURL, "/")
	cfg := AgentConfig{RelayURL: base, LLMSecret: sec.LLMSecret, InboundURL: base + "/inbound", WebhookURL: base + "/status", VoiceID: s.DefaultVoiceID, TTSModel: s.DefaultTTSModel}
	if s.AgentID == "" {
		if s.AgentID, err = el.CreateAgent(ctx, cfg); err != nil {
			return s, err
		}
	} else if err := el.UpdateAgent(ctx, s.AgentID, cfg); err != nil {
		return s, err
	}
	if s.PhoneNumberID == "" {
		if s.PhoneNumberID, err = el.ImportTwilioNumber(ctx, s.TwilioNumber, "Jarvis", sec.TwilioSID, sec.TwilioToken); err != nil {
			return s, err
		}
	}
	if err := el.AssignAgent(ctx, s.PhoneNumberID, s.AgentID); err != nil {
		return s, err
	}
	if s.PostCallWebhookID == "" {
		id, secret, err := el.CreatePostCallWebhook(ctx, cfg.WebhookURL, sec.LLMSecret)
		if err != nil {
			return s, err
		}
		if id == "" {
			return s, fmt.Errorf("ElevenLabs returned no webhook id")
		}
		s.PostCallWebhookID = id
		if secret != "" && sec.StoreWebhookSecret != nil {
			if err := sec.StoreWebhookSecret(secret); err != nil {
				return s, err
			}
		}
	}
	if err := el.SetWebhooks(ctx, cfg.InboundURL, sec.LLMSecret, s.PostCallWebhookID); err != nil {
		return s, err
	}
	return s, nil
}
