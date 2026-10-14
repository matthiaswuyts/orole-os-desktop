// Package jarvis lets bots phone the owner and the owner phone their bots. Calls run through
// ElevenLabs Agents (voice) and Twilio (number); each spoken turn reaches the daemon through an
// outbound WebSocket to a relay, so the daemon never listens on a network interface.
package jarvis

type Trigger string

const (
	TaskFinished        Trigger = "task_finished"
	TaskFailed          Trigger = "task_failed"
	AutomationFinished  Trigger = "automation_finished"
	AutomationFailed    Trigger = "automation_failed"
	NotificationTrigger Trigger = "notification"
	AtTime              Trigger = "at_time"
)

func (t Trigger) Valid() bool {
	switch t {
	case TaskFinished, TaskFailed, AutomationFinished, AutomationFailed, NotificationTrigger, AtTime:
		return true
	}
	return false
}

type Rule struct {
	ID            string  `json:"id"`
	BotID         string  `json:"botId"`
	Trigger       Trigger `json:"trigger"`
	Target        string  `json:"target"`
	Repeat        bool    `json:"repeat"`
	Urgent        bool    `json:"urgent"`
	QuietOverride bool    `json:"quietOverride"`
	Note          string  `json:"note"`
	ExpiresAt     int64   `json:"expiresAt,omitempty"`
	CreatedAt     int64   `json:"createdAt"`
}

const (
	Outbound = "outbound"
	Inbound  = "inbound"
)

// Call statuses.
const (
	StatusQueued     = "queued"
	StatusDialing    = "dialing"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusNoAnswer   = "no_answer"
	StatusVoicemail  = "voicemail"
	StatusFailed     = "failed"
	StatusRejected   = "rejected"
)

type Call struct {
	ID             string `json:"id"`
	BotID          string `json:"botId"`
	Direction      string `json:"direction"`
	RuleID         string `json:"ruleId,omitempty"`
	Reason         string `json:"reason"`
	Status         string `json:"status"`
	ProviderCallID string `json:"-"`
	ConversationID string `json:"-"`
	MessageID      string `json:"messageId,omitempty"`
	StartedAt      int64  `json:"startedAt,omitempty"`
	AnsweredAt     int64  `json:"answeredAt,omitempty"`
	EndedAt        int64  `json:"endedAt,omitempty"`
	EstCostMicros  int64  `json:"estCostMicros"`
	CreatedAt      int64  `json:"createdAt"`
}

// Settings is the plain (non-secret) global configuration, stored as JSON under settings key "jarvis".
type Settings struct {
	RelayURL          string `json:"relayUrl"`    // https://<worker>.workers.dev
	OwnerNumber       string `json:"ownerNumber"` // E.164
	OwnerVerified     bool   `json:"ownerVerified"`
	TwilioNumber      string `json:"twilioNumber"`      // E.164 of the Jarvis number
	AgentID           string `json:"agentId"`           // ElevenLabs agent, set by provisioning
	PhoneNumberID     string `json:"phoneNumberId"`     // ElevenLabs phone number id, set by provisioning
	PostCallWebhookID string `json:"postCallWebhookId"` // ElevenLabs workspace webhook id, set by provisioning
	QuietStart        string `json:"quietStart"`        // "22:00", "" = no quiet hours
	QuietEnd          string `json:"quietEnd"`          // "07:00"
	DailyCap          int    `json:"dailyCap"`          // 0 = default 10
	MonthlyCapCents   int    `json:"monthlyCapCents"`   // 0 = default 2000
	CentsPerMinute    int    `json:"centsPerMinute"`    // 0 = default 12
	DefaultVoiceID    string `json:"defaultVoiceId"`
	DefaultTTSModel   string `json:"defaultTtsModel"`
}

// SettingsKey is the settings table key for Settings.
const SettingsKey = "jarvis"

// Credential keys (internal/credentials adds the "Orole OS/" prefix).
const (
	KeyTwilioSID     = "jarvis/twilio/sid"
	KeyTwilioToken   = "jarvis/twilio/token"
	KeyElevenLabs    = "jarvis/elevenlabs/key"
	KeyRelayToken    = "jarvis/relay/token"      // daemon <-> relay
	KeyLLMSecret     = "jarvis/relay/llm-secret" // ElevenLabs -> relay header
	KeyWebhookSecret = "jarvis/relay/webhook"    // ElevenLabs post-call HMAC
)

func (s Settings) dailyCap() int        { return or(s.DailyCap, 10) }
func (s Settings) monthlyCapCents() int { return or(s.MonthlyCapCents, 2000) }
func (s Settings) centsPerMinute() int  { return or(s.CentsPerMinute, 12) }

func or(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}
