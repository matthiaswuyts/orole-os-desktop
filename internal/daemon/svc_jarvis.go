package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/jarvis"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

// jarvisCredentialStore holds the Twilio, ElevenLabs and relay secrets; tests replace it with memory.
var jarvisCredentialStore = credentials.New

// maxJarvisRules caps the call rules one bot may keep.
const maxJarvisRules = 50

// jarvisHost adapts the daemon to jarvis.Host.
type jarvisHost struct{ d *Daemon }

func (h jarvisHost) Bot(ctx context.Context, id string) (chat.Bot, error) {
	return h.d.Chat.Bot(ctx, id)
}
func (h jarvisHost) Bots(ctx context.Context) ([]chat.Bot, error) { return h.d.Chat.Bots(ctx) }

// Gate re-checks, on every call and spoken turn, that the bot still has Jarvis on and full permissions.
func (h jarvisHost) Gate(ctx context.Context, b chat.Bot) error {
	if !b.Config.Jarvis.Enabled {
		return &jarvis.GateError{Reason: "Jarvis mode is off for " + b.Name + "."}
	}
	return jarvis.CheckGate(h.d.jarvisGateInput(b.Config))
}

func (h jarvisHost) VoiceTurn(ctx context.Context, r chat.VoiceRequest) (string, error) {
	return h.d.Chat.VoiceTurn(ctx, r)
}

func (h jarvisHost) Compose(ctx context.Context, botID, in string) (string, error) {
	return h.d.Chat.Compose(ctx, botID, in)
}

func (h jarvisHost) PostCallCard(ctx context.Context, botID string, p chat.MsgPart) (string, error) {
	return h.d.Chat.PostCallCard(ctx, botID, p)
}

func (h jarvisHost) UpdateCallCard(ctx context.Context, id string, p chat.MsgPart) error {
	return h.d.Chat.UpdateCallCard(ctx, id, p)
}

// Notify records a notification with source "jarvis"; notifyResult never feeds those back into rules.
func (h jarvisHost) Notify(title, body, target, level string) {
	h.d.notices.notify(notifications.Input{Source: jarvisSource, Kind: notifications.Message, Title: title,
		Body: clipTo(body, noticeBody), Level: notifications.Level(level), Target: target})
}

func (h jarvisHost) Audit(action, target, outcome, detail string) {
	h.d.audit(action, target, outcome, detail)
}

// DisableJarvis switches Jarvis off for a bot whose permissions no longer pass the gate, and tells the owner.
func (h jarvisHost) DisableJarvis(ctx context.Context, botID, reason string) {
	b, err := h.d.Chat.Bot(ctx, botID)
	if err != nil || !b.Config.Jarvis.Enabled {
		return
	}
	cfg := b.Config
	cfg.Jarvis.Enabled = false
	in := chat.BotInput{ID: b.ID, Name: b.Name, Avatar: b.Avatar, Color: b.Color, Description: b.Description, Instructions: b.Instructions,
		Provider: b.Provider, Model: b.Model, Reasoning: b.Reasoning, Config: cfg}
	if _, err := h.d.Chat.SaveBot(ctx, in); err != nil {
		h.d.Log.Warn("jarvis: cannot switch off", "bot", botID, "err", err)
		return
	}
	h.d.audit("jarvis.disabled", botID, "allowed", reason)
	h.Notify("Jarvis turned off for "+b.Name, reason, "bot:"+botID, "warning")
}

const jarvisSource = "jarvis"

// jarvisGateInput resolves a bot's effective permissions the same way tool calls and task starts do.
// Unreadable agent settings fail closed.
func (d *Daemon) jarvisGateInput(c chat.BotConfig) jarvis.GateInput {
	v, ok := d.agentSettings()
	access := c.ToolAccess
	if access == "" {
		access = d.globalBotToolAccess()
	} else if !validToolAccess(access) {
		access = chat.ToolAccessAsk
	}
	return jarvis.GateInput{ToolAccess: access, TaskMode: d.botTaskDefaults(c.Tasks).PermissionMode,
		BotYolo: ok && v.BotYolo, YoloDisabled: !ok || v.YoloDisabled, CodeTasks: c.CodeTasks}
}

// jarvisSaveCheck runs before a bot with Jarvis on is saved: Jarvis needs full permissions, because phone
// calls have no approval prompt. Turning Jarvis on is refused when the gate fails; a bot that already has it on is
// saved with Jarvis switched off instead (so lowering a permission, granting a skill or saving a profile still works),
// and the owner is told.
func (d *Daemon) jarvisSaveCheck(ctx context.Context, stored *chat.Bot, in chat.BotInput) (chat.BotInput, error) {
	wasOn := stored != nil && stored.Config.Jarvis.Enabled
	if err := jarvis.CheckGate(d.jarvisGateInput(in.Config)); err != nil {
		if !wasOn {
			return in, &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
		}
		in.Config.Jarvis.Enabled = false
		d.audit("jarvis.disabled", in.ID, "allowed", err.Error())
		jarvisHost{d}.Notify("Jarvis turned off for "+in.Name, err.Error(), "bot:"+in.ID, "warning")
		return in, nil
	}
	if !wasOn {
		// The save itself may still fail; this records only that the permission check passed.
		d.audit("jarvis.enable.checked", in.ID, "allowed", "permission check passed for "+in.Name)
	}
	return in, nil
}

// jarvisSweep switches Jarvis off for every bot whose permissions no longer pass the gate (after the global
// agents setting changed), with the same audit entry and notice as a mid-call gate failure.
func (d *Daemon) jarvisSweep(ctx context.Context) {
	if d.Chat == nil {
		return
	}
	bots, err := d.Chat.Bots(ctx)
	if err != nil {
		d.Log.Warn("jarvis: cannot list bots for the permission sweep", "err", err)
		return
	}
	for _, b := range bots {
		if !b.Config.Jarvis.Enabled {
			continue
		}
		if err := jarvis.CheckGate(d.jarvisGateInput(b.Config)); err != nil {
			jarvisHost{d}.DisableJarvis(ctx, b.ID, err.Error())
		}
	}
}

// jarvisTools adapts the service to tools.JarvisRuntime; bots may only touch their own rules.
// It reads d.Jarvis at call time because registerChatTools runs before registerJarvis.
type jarvisTools struct{ d *Daemon }

func (t jarvisTools) svc() (*jarvis.Service, error) {
	if t.d.Jarvis == nil {
		return nil, errors.New("Phone calls are not available.")
	}
	return t.d.Jarvis, nil
}

func (t jarvisTools) AddRule(ctx context.Context, botID string, r tools.JarvisRule) (tools.JarvisRule, error) {
	s, err := t.svc()
	if err != nil {
		return tools.JarvisRule{}, err
	}
	if have, err := s.Rules(ctx, botID); err != nil {
		return tools.JarvisRule{}, err
	} else if len(have) >= maxJarvisRules {
		return tools.JarvisRule{}, errors.New("You already have the maximum number of call rules; delete one first.")
	}
	got, err := s.AddRule(ctx, jarvis.JarvisRule{BotID: botID, Trigger: jarvis.JarvisTrigger(r.Trigger), Target: r.Target, Note: r.Note,
		Repeat: r.Repeat, Urgent: r.Urgent, QuietOverride: r.QuietOverride, ExpiresAt: r.ExpiresAt})
	if err != nil {
		return tools.JarvisRule{}, err
	}
	t.d.audit("jarvis.rule.add", got.ID, "allowed", botID+" "+string(got.Trigger)+" "+got.Target)
	t.d.jarvisChanged()
	return toToolRule(got), nil
}

func (t jarvisTools) Rules(ctx context.Context, botID string) ([]tools.JarvisRule, error) {
	s, err := t.svc()
	if err != nil {
		return nil, err
	}
	rs, err := s.Rules(ctx, botID)
	out := make([]tools.JarvisRule, 0, len(rs))
	for _, r := range rs {
		out = append(out, toToolRule(r))
	}
	return out, err
}

func (t jarvisTools) DeleteRule(ctx context.Context, botID, id string) error {
	s, err := t.svc()
	if err != nil {
		return err
	}
	rs, err := s.Rules(ctx, botID)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.ID == id {
			if err := s.DeleteRule(ctx, id); err != nil {
				return err
			}
			t.d.audit("jarvis.rule.delete", id, "allowed", botID)
			t.d.jarvisChanged()
			return nil
		}
	}
	return errors.New("You have no rule with that id.")
}

func (t jarvisTools) CallOwner(ctx context.Context, botID, reason string, urgent bool) (string, error) {
	s, err := t.svc()
	if err != nil {
		return "", err
	}
	return s.CallOwner(ctx, botID, reason, urgent)
}

func (t jarvisTools) SwitchBot(ctx context.Context, callID, name string) (string, error) {
	s, err := t.svc()
	if err != nil {
		return "", err
	}
	return s.SwitchBot(ctx, callID, name)
}

func toToolRule(r jarvis.JarvisRule) tools.JarvisRule {
	return tools.JarvisRule{ID: r.ID, Trigger: string(r.Trigger), Target: r.Target, Note: r.Note, Repeat: r.Repeat, Urgent: r.Urgent,
		QuietOverride: r.QuietOverride, ExpiresAt: r.ExpiresAt}
}

func (d *Daemon) loadJarvisSettings() (jarvis.PhoneSettings, error) {
	var raw string
	var s jarvis.PhoneSettings
	err := d.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, jarvis.SettingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal([]byte(raw), &s)
}

// jarvisStateKey holds daemon-owned Jarvis state (the last at_time tick); settings.set cannot write it.
const jarvisStateKey = "jarvis.state"

// jarvisProtectedKey reports whether settings.set must refuse key: Jarvis settings change only through jarvis.*.
func jarvisProtectedKey(key string) bool { return key == jarvis.SettingsKey || key == jarvisStateKey }

func (d *Daemon) loadJarvisState() (jarvis.State, error) {
	var raw string
	var s jarvis.State
	err := d.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, jarvisStateKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal([]byte(raw), &s)
}

func (d *Daemon) saveJarvisState(s jarvis.State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		jarvisStateKey, string(b), time.Now().Unix())
	return err
}

func (d *Daemon) saveJarvisSettings(s jarvis.PhoneSettings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		jarvis.SettingsKey, string(b), time.Now().Unix())
	return err
}

func (d *Daemon) jarvisChanged() {
	if d.Jarvis != nil {
		d.IPC.Publish("jarvis.changed", d.Jarvis.Status(context.Background()))
	}
}

// jarvisErr turns refusals (setup missing, policy, bad input) into user-facing bad-request errors.
func jarvisErr(err error) error {
	var r *jarvis.Refusal
	var g *jarvis.GateError
	if errors.As(err, &r) || errors.As(err, &g) {
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	}
	return err
}

// jarvisTaskEvent maps a finished or failed task to a rule event.
func jarvisTaskEvent(topic string, s agents.Session) (jarvis.Event, bool) {
	var trig jarvis.JarvisTrigger
	var what string
	switch topic {
	case events.TopicAgentFinished:
		trig, what = jarvis.TaskFinished, " finished"
	case events.TopicAgentFailed:
		trig, what = jarvis.TaskFailed, " failed"
	default:
		return jarvis.Event{}, false
	}
	title := s.Title
	if title == "" {
		title = s.ID
	}
	owner, _ := strings.CutPrefix(s.Owner, chat.BotPrefix)
	if owner == s.Owner {
		owner = "" // not a bot
	}
	return jarvis.Event{Trigger: trig, TargetID: s.ID, OwnerBotID: owner, Title: "Task " + title + what, Detail: taskReportText(topic, s)}, true
}

func (d *Daemon) jarvisTask(topic string, s agents.Session) {
	if d.Jarvis == nil {
		return
	}
	if ev, ok := jarvisTaskEvent(topic, s); ok {
		d.Jarvis.OnEvent(ev)
	}
}

// terminalRuns remembers automation runs already forwarded to Jarvis: the engine reports every journal change,
// and a run must reach the rules once.
type terminalRuns struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newTerminalRuns() *terminalRuns { return &terminalRuns{seen: map[string]time.Time{}} }

func (t *terminalRuns) first(id string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, at := range t.seen {
		if now.Sub(at) > time.Hour {
			delete(t.seen, k)
		}
	}
	if _, ok := t.seen[id]; ok {
		return false
	}
	t.seen[id] = now
	return true
}

func (d *Daemon) jarvisRun(seen *terminalRuns, r automations.Run) {
	if d.Jarvis == nil {
		return
	}
	var trig jarvis.JarvisTrigger
	switch r.Status {
	case "succeeded":
		trig = jarvis.AutomationFinished
	case "failed", "uncertain":
		trig = jarvis.AutomationFailed
	default:
		return
	}
	if !seen.first(r.ID, time.Now()) {
		return
	}
	name := r.Name
	if name == "" {
		name = "Flow"
	}
	d.Jarvis.OnEvent(jarvis.Event{Trigger: trig, TargetID: r.AutomationID, Title: "Automation " + name + " " + r.Status, Detail: r.Error})
}

// jarvisNotification forwards a stored notification to the rules, except Jarvis's own.
func (d *Daemon) jarvisNotification(n notifications.Notification) {
	if d.Jarvis == nil || n.Source == jarvisSource {
		return
	}
	d.Jarvis.OnEvent(jarvis.Event{Trigger: jarvis.NotificationTrigger, TargetID: string(n.Kind), Source: n.Source, Title: n.Title, Detail: n.Body})
}

// registerJarvis runs after registerChatTools: the service needs the chat Manager.
func (d *Daemon) registerJarvis() {
	if d.Chat == nil {
		return
	}
	d.Jarvis = jarvis.NewService(jarvis.ServiceConfig{DB: d.DB.DB, Creds: jarvisCredentialStore(), Host: jarvisHost{d}, Log: d.Log,
		HTTP: llm.NewHTTPClient(), LoadSettings: d.loadJarvisSettings, SaveSettings: d.saveJarvisSettings,
		LoadState: d.loadJarvisState, SaveState: d.saveJarvisState,
		Changed: func(v jarvis.StatusView) { d.IPC.Publish("jarvis.changed", v) }})
	d.Chat.SetSaveCheck(d.jarvisSaveCheck)

	handle(d, "jarvis.status", func(ctx context.Context, _ none) (jarvis.StatusView, error) { return d.Jarvis.Status(ctx), nil })
	handle(d, "jarvis.secrets.set", func(ctx context.Context, p jarvis.SecretsInput) (jarvis.StatusView, error) {
		if err := d.Jarvis.SetSecrets(p); err != nil {
			return jarvis.StatusView{}, jarvisErr(err)
		}
		d.audit("jarvis.secrets.set", "", "allowed", secretNames(p))
		d.jarvisChanged()
		return d.Jarvis.Status(ctx), nil
	})
	handle(d, "jarvis.relaySecrets.generate", func(ctx context.Context, _ none) (jarvis.RelaySecrets, error) {
		out, err := d.Jarvis.GenerateRelaySecrets()
		if err != nil {
			return jarvis.RelaySecrets{}, err
		}
		d.audit("jarvis.relay.secrets", "", "allowed", "generated")
		d.jarvisChanged()
		return out, nil
	})
	handle(d, "jarvis.configure", func(ctx context.Context, p jarvis.PhoneSettings) (jarvis.ConfigureResult, error) {
		res, err := d.Jarvis.Configure(ctx, p)
		d.jarvisChanged() // a failed run may still have saved what it created
		if err != nil {
			d.audit("jarvis.configure", "", "error", "")
			return jarvis.ConfigureResult{}, jarvisErr(err)
		}
		d.audit("jarvis.configure", "", "allowed", "")
		return res, nil
	})
	handle(d, "jarvis.verify.start", func(ctx context.Context, p struct {
		Number string `json:"number"`
	}) (none, error) {
		err := d.Jarvis.StartVerification(ctx, p.Number)
		d.jarvisChanged()
		return none{}, jarvisErr(err)
	})
	handle(d, "jarvis.verify.confirm", func(ctx context.Context, p struct {
		Code string `json:"code"`
	}) (jarvis.StatusView, error) {
		if err := d.Jarvis.ConfirmVerification(p.Code); err != nil {
			d.jarvisChanged()
			return jarvis.StatusView{}, jarvisErr(err)
		}
		d.jarvisChanged()
		return d.Jarvis.Status(ctx), nil
	})
	handle(d, "jarvis.voices", func(ctx context.Context, _ none) ([]jarvis.Voice, error) {
		v, err := d.Jarvis.Voices(ctx)
		return v, jarvisErr(err)
	})
	handle(d, "jarvis.models", func(ctx context.Context, _ none) ([]jarvis.TTSModel, error) {
		v, err := d.Jarvis.Models(ctx)
		return v, jarvisErr(err)
	})
	handle(d, "jarvis.rules.list", func(ctx context.Context, p struct {
		BotID string `json:"botId"`
	}) ([]jarvis.JarvisRule, error) {
		rs, err := d.Jarvis.Rules(ctx, p.BotID)
		if rs == nil {
			rs = []jarvis.JarvisRule{}
		}
		return rs, err
	})
	handle(d, "jarvis.rules.delete", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		if err := d.Jarvis.DeleteRule(ctx, p.ID); err != nil {
			return none{}, jarvisErr(err)
		}
		d.audit("jarvis.rule.delete", p.ID, "allowed", "desktop")
		d.jarvisChanged()
		return none{}, nil
	})
	handle(d, "jarvis.calls.transcript", func(ctx context.Context, p struct {
		CallID string `json:"callId"`
	}) (jarvis.CallTranscript, error) {
		return d.jarvisTranscript(ctx, p.CallID)
	})
	handle(d, "jarvis.call.test", func(ctx context.Context, p struct {
		BotID string `json:"botId"`
	}) (string, error) {
		msg, err := d.Jarvis.TestCall(ctx, p.BotID)
		d.jarvisChanged()
		return msg, jarvisErr(err)
	})
}

// secretNames lists which credentials were set, for the audit log; never the values.
func secretNames(p jarvis.SecretsInput) string {
	var names []string
	if strings.TrimSpace(p.TwilioSID) != "" {
		names = append(names, "twilio sid")
	}
	if strings.TrimSpace(p.TwilioToken) != "" {
		names = append(names, "twilio token")
	}
	if strings.TrimSpace(p.ElevenLabs) != "" {
		names = append(names, "elevenlabs key")
	}
	return strings.Join(names, ", ")
}

// jarvisTranscript returns a call and its spoken turns, which the chat timeline hides (chat.Message.CallID).
func (d *Daemon) jarvisTranscript(ctx context.Context, callID string) (jarvis.CallTranscript, error) {
	if callID == "" {
		return jarvis.CallTranscript{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "A call id is required."}
	}
	c, err := d.Jarvis.Call(ctx, callID)
	if errors.Is(err, jarvis.ErrNotFound) {
		return jarvis.CallTranscript{}, &ipc.Error{Code: ipc.CodeNotFound, Message: "That call was not found."}
	}
	if err != nil {
		return jarvis.CallTranscript{}, err
	}
	msgs, err := d.Chat.CallMessages(ctx, c.ID)
	if err != nil {
		return jarvis.CallTranscript{}, err
	}
	return jarvis.CallTranscript{Call: c, Messages: msgs}, nil
}
