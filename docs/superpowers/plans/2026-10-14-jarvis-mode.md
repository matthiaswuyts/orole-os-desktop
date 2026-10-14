# Jarvis Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a full-permission bot phone the owner (on rules or its own judgement) and let the owner phone their bots, with the bot's own model and tools answering live.

**Architecture:** Twilio number + ElevenLabs Agents (speech, turn-taking, TTS) with a *custom LLM* URL pointing at a Cloudflare Worker relay. The daemon keeps one outbound WebSocket to the relay; each spoken turn is forwarded to the daemon, run through the chat engine in a new voice mode, and streamed back. A new `internal/jarvis` package owns rules, the permission gate, call policy, call lifecycle, provider clients and the relay client.

**Tech Stack:** Go 1.27 (stdlib, `modernc.org/sqlite`, new dep `github.com/coder/websocket`), TypeScript Cloudflare Worker + Durable Object (`relay/`, wrangler 4, vitest with `@cloudflare/vitest-pool-workers`), React 19 + Zustand + Base UI (frontend), MyGo bindings.

**Spec:** `docs/superpowers/specs/2026-10-14-jarvis-mode-design.md`

## Spec amendments (confirm with owner before Task 1)

1. **Call transcript (§6.4).** Spoken turns are stored as ordinary user/assistant messages in the bot's thread, bracketed by one `PartCall` card message that is created at call start and updated at call end (status, duration, cost). Reuses the chat store and renders the transcript as chat.
2. **Voicemail and call status (§6.1).** Use ElevenLabs' `voicemail_detection` system tool and the ElevenLabs post-call webhook plus conversation polling. No Twilio status callbacks; the relay does not verify Twilio signatures.
3. **STIR/SHAKEN (§6.2 step 3) is dropped.** ElevenLabs' native Twilio inbound webhook passes `caller_id`, `called_number`, `call_sid`, `agent_id` only. Inbound acceptance is caller-ID match alone (the owner's stated choice).

Update the spec file with these three amendments in Task 1's commit.

## Global Constraints

- No listener on a network interface. The daemon only dials out (WSS to the relay, HTTPS to providers).
- Secrets only in Windows Credential Manager via `internal/credentials` (keys under `jarvis/…`). Never SQLite, logs, IPC payloads to the frontend, or env dumps.
- Permissions decided in Go. Jarvis requires: effective tool access `all`, effective code-task mode `yolo`, `agents.botYolo` on and `agents.yoloDisabled` off, `BotConfig.CodeTasks` true.
- Spoken output never contains "Orole". Every opener contains the bot's name.
- Provider content (transcripts, webhook payloads, task output) is untrusted data, fenced with `chat.Fence`.
- Unconfigured integration shows a setup screen; no mock data in production paths.
- Jarvis tables use unix **milliseconds**; audit uses the existing `d.audit` (unix seconds).
- Commits: conventional, lowercase, e.g. `feat(jarvis): …`. Branch `feat/<issue>-jarvis-mode`. Every commit ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After changing anything bound in `internal/app`: `go tool mygo generate` and commit `frontend/src/mygo.ts`.
- Go checks: `go fmt ./... && go vet ./... && go test -race ./...`. Frontend: `bun run --cwd frontend check`.
- No native HTML `title` attributes; hover text via `Tooltip` only where needed.
- Defaults: daily call cap 10, monthly cost cap 2000 cents, cool-down 10 min (judgement calls only), cost estimate 12 cents/min, socket-drop grace 5 s, urgent retry once after 5 min, opener ≤ 240 chars, policy text ≤ 2000 chars, rule note ≤ 280 chars, voice turn step limit 6 and tool-call limit 10.

## Review Focus

1. **The bot is mid-turn in chat when a call starts or a spoken turn arrives.** Voice turns must not wait behind the bot's chat queue; they run under their own run key `call:<callID>` (Task 7 test `TestVoiceTurnRunsWhileChatTurnBusy`).
2. **Two rules fire for the same event, or a rule fires during a live call.** One call only; the second becomes a live-call injection, never a second dial (Task 10 test `TestSecondRuleDuringCallIsInjected`).
3. **The owner lowers the bot's permissions while Jarvis is on.** The next call or action is refused, Jarvis is switched off for that bot, and a notification says why (Task 10 test `TestGateFailureMidCallDisablesJarvis`).
4. **The daemon restarts with `once` rules and an `at_time` rule in the past.** Past `at_time` rules that were missed while down become a notification, not a late call (Task 5 test `TestMissedAtTimeBecomesNotice`).
5. **ElevenLabs sends a request for a call the daemon no longer knows (after restart).** The relay/daemon answer with a short spoken goodbye and end; no panic, no tools (Task 10 test `TestUnknownCallGetsGoodbye`).

---

## File Structure

```
migrations/0109_jarvis.sql                       rules + calls tables
internal/jarvis/
  types.go          Settings, Rule, Call, trigger/status constants, secret key names
  store.go          SQLite access for rules and calls
  gate.go           permission gate (pure)
  policy.go         quiet hours, caps, cool-down (pure)
  opener.go         opener check + fallbacks (pure)
  rules.go          Event, rule matching, at_time scheduling (pure)
  relay.go          relay frame types + reconnecting WebSocket client
  elevenlabs.go     ElevenLabs REST client (agent, phone number, outbound call, voices, models, conversation)
  twilio.go         Twilio REST client (validate, list numbers)
  provision.go      creates/updates the ElevenLabs agent + phone number
  calls.go          call manager: lifecycle, llm.request handling, inbound, fillers, injections
  service.go        Service: ties store + gate + policy + calls; event intake; settings get/set
  *_test.go
internal/chat/
  api.go            + BotJarvis, BotConfig.Jarvis, PartCall, RunContext.CallID (tool.go)
  store.go          + Jarvis validation
  manager.go        + SetSaveCheck hook, ImportBot clears Jarvis
  prompt.go         + voiceStyle, systemInput.Voice
  engine.go         + prep/turnEnv voice fields, onText/onTool hooks, voice reasoning + limits
  voice.go          VoiceTurn, Compose, PostCallCard, UpdateCallCard
internal/chat/tools/jarvis.go                    jarvis_rule_add/list/delete, call_owner, switch_bot
internal/daemon/svc_jarvis.go                    wiring, IPC methods, event hooks, adapters
internal/app/jarvis.go                           desktop service + events
cmd/desktop/main.go                              bind app.Jarvis
relay/                                           Cloudflare Worker (src/index.ts, src/hub.ts, test/, wrangler.toml)
frontend/src/features/jarvis/                    PhoneSettings.tsx, JarvisPage.tsx, CallCard.tsx, store.ts, tests
docs/architecture/jarvis.md, docs/security/threat-model.md, docs/architecture/chat.md,
docs/architecture/database-schema.md, CHANGELOG.md, docs/development/jarvis-e2e.md
```

---

### Task 1: Migration, types and store

**Files:**
- Create: `migrations/0109_jarvis.sql`, `internal/jarvis/types.go`, `internal/jarvis/store.go`, `internal/jarvis/store_test.go`
- Modify: `docs/superpowers/specs/2026-10-14-jarvis-mode-design.md` (amendments above)

**Interfaces:**
- Produces:
  - `type Trigger string` with `TaskFinished, TaskFailed, AutomationFinished, AutomationFailed, NotificationTrigger, AtTime`
  - `type Rule struct{ID, BotID string; Trigger Trigger; Target string; Repeat, Urgent, QuietOverride bool; Note string; ExpiresAt, CreatedAt int64}`
  - `type Call struct{ID, BotID, Direction, RuleID, Reason, Status, ProviderCallID, ConversationID, MessageID string; StartedAt, AnsweredAt, EndedAt, EstCostMicros, CreatedAt int64}`
  - `func NewStore(db *sql.DB, now func() time.Time) *Store`
  - `(*Store) AddRule(ctx, Rule) (Rule, error)`, `Rules(ctx, botID string) ([]Rule, error)` ("" = all), `DeleteRule(ctx, id string) error`, `InsertCall(ctx, *Call) error`, `UpdateCall(ctx, Call) error`, `GetCall(ctx, id) (Call, error)`, `RecentCalls(ctx, botID string, limit int) ([]Call, error)`, `CallsSince(ctx, ms int64) (count int, costMicros int64, err error)`, `LastCallAt(ctx, botID string) (int64, error)`
  - `var ErrNotFound`

- [ ] **Step 1: Write the migration**

```sql
-- Jarvis mode: rules tell the daemon when a bot phones the owner; calls record each phone call.
-- Times are unix milliseconds. target is a task or automation id, "any", a notification
-- kind[:source], or HH:MM for at_time. Once rules (repeat=0) are deleted after they fire.
CREATE TABLE jarvis_rules (
  id             TEXT PRIMARY KEY,
  bot_id         TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  trigger        TEXT NOT NULL CHECK (trigger IN ('task_finished','task_failed','automation_finished','automation_failed','notification','at_time')),
  target         TEXT NOT NULL DEFAULT '' CHECK (length(target) <= 200),
  repeat         INTEGER NOT NULL DEFAULT 0,
  urgent         INTEGER NOT NULL DEFAULT 0,
  quiet_override INTEGER NOT NULL DEFAULT 0,
  note           TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 280),
  expires_at     INTEGER,
  created_at     INTEGER NOT NULL
);
CREATE INDEX jarvis_rules_trigger ON jarvis_rules(trigger);
CREATE TABLE jarvis_calls (
  id               TEXT PRIMARY KEY,
  bot_id           TEXT,
  direction        TEXT NOT NULL CHECK (direction IN ('outbound','inbound')),
  rule_id          TEXT,
  reason           TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL,
  provider_call_id TEXT NOT NULL DEFAULT '',
  conversation_id  TEXT NOT NULL DEFAULT '',
  message_id       TEXT NOT NULL DEFAULT '',
  started_at       INTEGER,
  answered_at      INTEGER,
  ended_at         INTEGER,
  est_cost_micros  INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL
);
CREATE INDEX jarvis_calls_created ON jarvis_calls(created_at);
CREATE INDEX jarvis_calls_bot ON jarvis_calls(bot_id, created_at);
```

If `0109` is taken on `main` when you start, use the next free number.

- [ ] **Step 2: Write `types.go`**

```go
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
	RelayURL           string `json:"relayUrl"`           // https://<worker>.workers.dev
	OwnerNumber        string `json:"ownerNumber"`        // E.164
	OwnerVerified      bool   `json:"ownerVerified"`
	TwilioNumber       string `json:"twilioNumber"`       // E.164 of the Jarvis number
	AgentID            string `json:"agentId"`            // ElevenLabs agent, set by provisioning
	PhoneNumberID      string `json:"phoneNumberId"`      // ElevenLabs phone number id, set by provisioning
	QuietStart         string `json:"quietStart"`         // "22:00", "" = no quiet hours
	QuietEnd           string `json:"quietEnd"`           // "07:00"
	DailyCap           int    `json:"dailyCap"`           // 0 = default 10
	MonthlyCapCents    int    `json:"monthlyCapCents"`    // 0 = default 2000
	CentsPerMinute     int    `json:"centsPerMinute"`     // 0 = default 12
	DefaultVoiceID     string `json:"defaultVoiceId"`
	DefaultTTSModel    string `json:"defaultTtsModel"`
}

// SettingsKey is the settings table key for Settings.
const SettingsKey = "jarvis"

// Credential keys (internal/credentials adds the "Orole OS/" prefix).
const (
	KeyTwilioSID      = "jarvis/twilio/sid"
	KeyTwilioToken    = "jarvis/twilio/token"
	KeyElevenLabs     = "jarvis/elevenlabs/key"
	KeyRelayToken     = "jarvis/relay/token"      // daemon <-> relay
	KeyLLMSecret      = "jarvis/relay/llm-secret" // ElevenLabs -> relay header
	KeyWebhookSecret  = "jarvis/relay/webhook"    // ElevenLabs post-call HMAC
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
```

- [ ] **Step 3: Write the failing store test**

```go
package jarvis

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func testStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO bots(id,name,created_at,updated_at) VALUES('b1','Atlas',0,0),('b2','Nova',0,0)`); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_000_000)
	return NewStore(db.DB, func() time.Time { return now }), &now
}

func TestRulesRoundTrip(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	r, err := s.AddRule(ctx, Rule{BotID: "b1", Trigger: TaskFinished, Target: "any", Note: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.CreatedAt != 1_000_000 {
		t.Fatalf("rule not stamped: %+v", r)
	}
	if _, err := s.AddRule(ctx, Rule{BotID: "b2", Trigger: AtTime, Target: "08:00", Repeat: true}); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.Rules(ctx, "b1")
	all, _ := s.Rules(ctx, "")
	if len(mine) != 1 || len(all) != 2 || mine[0].Note != "deploy" {
		t.Fatalf("mine=%v all=%v", mine, all)
	}
	if err := s.DeleteRule(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRule(ctx, r.ID); err != ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}
}

func TestAddRuleRejectsBadInput(t *testing.T) {
	s, _ := testStore(t)
	for _, r := range []Rule{
		{BotID: "b1", Trigger: "bogus"},
		{BotID: "b1", Trigger: AtTime, Target: "25:00"},
		{BotID: "b1", Trigger: TaskFailed, Target: ""},
		{BotID: "b1", Trigger: TaskFailed, Target: "any", Note: string(make([]byte, 281))},
	} {
		if _, err := s.AddRule(context.Background(), r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
}

func TestCallsSinceSumsCost(t *testing.T) {
	s, now := testStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		c := Call{BotID: "b1", Direction: Outbound, Status: StatusCompleted, EstCostMicros: 100_000}
		if err := s.InsertCall(ctx, &c); err != nil {
			t.Fatal(err)
		}
	}
	n, cost, err := s.CallsSince(ctx, now.UnixMilli()-1)
	if err != nil || n != 3 || cost != 300_000 {
		t.Fatalf("n=%d cost=%d err=%v", n, cost, err)
	}
	last, _ := s.LastCallAt(ctx, "b1")
	if last != now.UnixMilli() {
		t.Fatalf("last=%d", last)
	}
}
```

- [ ] **Step 4: Run it and see it fail**

Run: `go test ./internal/jarvis/ -run 'TestRules|TestAddRule|TestCalls' -v`
Expected: FAIL, `undefined: NewStore`.

- [ ] **Step 5: Implement `store.go`**

```go
package jarvis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("jarvis: not found")

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

func validateRule(r Rule) error {
	if r.BotID == "" {
		return errors.New("A rule needs a bot.")
	}
	if !r.Trigger.Valid() {
		return fmt.Errorf("Unknown trigger %q.", r.Trigger)
	}
	if r.Trigger == AtTime && !hhmm.MatchString(r.Target) {
		return errors.New("A time rule needs a time like 08:30.")
	}
	if r.Trigger != AtTime && r.Target == "" {
		return errors.New("A rule needs a target id or \"any\".")
	}
	if len(r.Target) > 200 || len(r.Note) > 280 {
		return errors.New("The rule's target or note is too long.")
	}
	return nil
}

func (s *Store) AddRule(ctx context.Context, r Rule) (Rule, error) {
	if err := validateRule(r); err != nil {
		return Rule{}, err
	}
	r.ID = uuid.NewString()
	r.CreatedAt = s.now().UnixMilli()
	var exp any
	if r.ExpiresAt > 0 {
		exp = r.ExpiresAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO jarvis_rules(id,bot_id,trigger,target,repeat,urgent,quiet_override,note,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, r.ID, r.BotID, string(r.Trigger), r.Target, r.Repeat, r.Urgent, r.QuietOverride, r.Note, exp, r.CreatedAt)
	return r, err
}

func (s *Store) Rules(ctx context.Context, botID string) ([]Rule, error) {
	q := `SELECT id,bot_id,trigger,target,repeat,urgent,quiet_override,note,COALESCE(expires_at,0),created_at FROM jarvis_rules`
	args := []any{}
	if botID != "" {
		q += ` WHERE bot_id=?`
		args = append(args, botID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var trig string
		if err := rows.Scan(&r.ID, &r.BotID, &trig, &r.Target, &r.Repeat, &r.Urgent, &r.QuietOverride, &r.Note, &r.ExpiresAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Trigger = Trigger(trig)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jarvis_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const callCols = `id,COALESCE(bot_id,''),direction,COALESCE(rule_id,''),reason,status,provider_call_id,conversation_id,message_id,
	COALESCE(started_at,0),COALESCE(answered_at,0),COALESCE(ended_at,0),est_cost_micros,created_at`

func scanCall(sc interface{ Scan(...any) error }) (Call, error) {
	var c Call
	err := sc.Scan(&c.ID, &c.BotID, &c.Direction, &c.RuleID, &c.Reason, &c.Status, &c.ProviderCallID, &c.ConversationID,
		&c.MessageID, &c.StartedAt, &c.AnsweredAt, &c.EndedAt, &c.EstCostMicros, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (s *Store) InsertCall(ctx context.Context, c *Call) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.CreatedAt = s.now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO jarvis_calls(id,bot_id,direction,rule_id,reason,status,provider_call_id,conversation_id,message_id,started_at,answered_at,ended_at,est_cost_micros,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, nullStr(c.BotID), c.Direction, nullStr(c.RuleID), c.Reason, c.Status, c.ProviderCallID,
		c.ConversationID, c.MessageID, nullInt(c.StartedAt), nullInt(c.AnsweredAt), nullInt(c.EndedAt), c.EstCostMicros, c.CreatedAt)
	return err
}

func (s *Store) UpdateCall(ctx context.Context, c Call) error {
	res, err := s.db.ExecContext(ctx, `UPDATE jarvis_calls SET bot_id=?,status=?,provider_call_id=?,conversation_id=?,message_id=?,
		started_at=?,answered_at=?,ended_at=?,est_cost_micros=? WHERE id=?`, nullStr(c.BotID), c.Status, c.ProviderCallID, c.ConversationID,
		c.MessageID, nullInt(c.StartedAt), nullInt(c.AnsweredAt), nullInt(c.EndedAt), c.EstCostMicros, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetCall(ctx context.Context, id string) (Call, error) {
	return scanCall(s.db.QueryRowContext(ctx, `SELECT `+callCols+` FROM jarvis_calls WHERE id=?`, id))
}

func (s *Store) RecentCalls(ctx context.Context, botID string, limit int) ([]Call, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+callCols+` FROM jarvis_calls WHERE (?='' OR bot_id=?) ORDER BY created_at DESC LIMIT ?`, botID, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CallsSince counts placed calls (inbound and outbound, any status except rejected) created at or after ms.
func (s *Store) CallsSince(ctx context.Context, ms int64) (int, int64, error) {
	var n int
	var cost int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(est_cost_micros),0) FROM jarvis_calls WHERE created_at>=? AND status<>?`,
		ms, StatusRejected).Scan(&n, &cost)
	return n, cost, err
}

func (s *Store) LastCallAt(ctx context.Context, botID string) (int64, error) {
	var at sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM jarvis_calls WHERE bot_id=? AND direction=?`, botID, Outbound).Scan(&at)
	return at.Int64, err
}
```

- [ ] **Step 6: Run the tests until they pass**

Run: `go test ./internal/jarvis/ -race -v`
Expected: PASS (3 tests).

- [ ] **Step 7: Apply the spec amendments**

In the spec: §6.4 replace the first sentence with "Each call gets one assistant message with a `PartCall` card, created at call start and updated at call end; the spoken turns are stored between it as ordinary user/assistant messages." In §6.1 step 6 replace "Answering-machine detection" with "ElevenLabs voicemail detection (system tool `voicemail_detection`)". Delete §6.2 step 3 and the STIR/SHAKEN wording in §10, adding "Inbound acceptance is caller-ID match only."

- [ ] **Step 8: Commit**

```bash
git add migrations/0109_jarvis.sql internal/jarvis/ docs/superpowers/specs/2026-10-14-jarvis-mode-design.md
git commit -m "feat(jarvis): add rules and calls storage"
```

---

### Task 2: Permission gate, call policy, opener check

**Files:**
- Create: `internal/jarvis/gate.go`, `internal/jarvis/policy.go`, `internal/jarvis/opener.go`, `internal/jarvis/pure_test.go`

**Interfaces:**
- Produces:
  - `type GateInput struct{ToolAccess, TaskMode string; BotYolo, YoloDisabled, CodeTasks bool}`
  - `type GateError struct{Reason string}` (implements `error`)
  - `func CheckGate(in GateInput) error`
  - `type Usage struct{Today int; MonthCostMicros int64; LastBotCallMs int64; InCall bool}`
  - `type Ask struct{Judgement, QuietOverride bool}`
  - `type Refusal struct{Reason string}` (implements `error`)
  - `func Allow(s Settings, now time.Time, u Usage, a Ask) error`
  - `func InQuietHours(s Settings, now time.Time) bool`
  - `func CheckOpener(name, text string) (string, bool)`
  - `func FallbackOpener(name, reason string, seed int) string`

- [ ] **Step 1: Write the failing tests**

```go
package jarvis

import (
	"strings"
	"testing"
	"time"
)

func TestCheckGate(t *testing.T) {
	full := GateInput{ToolAccess: "all", TaskMode: "yolo", BotYolo: true, CodeTasks: true}
	if err := CheckGate(full); err != nil {
		t.Fatalf("full: %v", err)
	}
	cases := map[string]func(*GateInput){
		"tool access":  func(g *GateInput) { g.ToolAccess = "write" },
		"task mode":    func(g *GateInput) { g.TaskMode = "auto" },
		"bot yolo":     func(g *GateInput) { g.BotYolo = false },
		"yolo off":     func(g *GateInput) { g.YoloDisabled = true },
		"code tasks":   func(g *GateInput) { g.CodeTasks = false },
	}
	for name, mod := range cases {
		g := full
		mod(&g)
		err := CheckGate(g)
		var ge *GateError
		if err == nil || !errorsAs(err, &ge) || ge.Reason == "" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAllowPolicy(t *testing.T) {
	loc := time.FixedZone("x", 0)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 14, h, m, 0, 0, loc) }
	s := Settings{QuietStart: "22:00", QuietEnd: "07:00"}
	if err := Allow(s, at(12, 0), Usage{}, Ask{}); err != nil {
		t.Fatalf("noon: %v", err)
	}
	if Allow(s, at(23, 0), Usage{}, Ask{}) == nil {
		t.Error("quiet hours not enforced at 23:00")
	}
	if Allow(s, at(6, 59), Usage{}, Ask{}) == nil {
		t.Error("quiet hours not enforced at 06:59 (wraps midnight)")
	}
	if err := Allow(s, at(23, 0), Usage{}, Ask{QuietOverride: true}); err != nil {
		t.Errorf("override: %v", err)
	}
	if Allow(s, at(12, 0), Usage{Today: 10}, Ask{}) == nil {
		t.Error("daily cap 10 not enforced")
	}
	if Allow(s, at(12, 0), Usage{MonthCostMicros: 20_000_000}, Ask{}) == nil {
		t.Error("monthly cap $20 not enforced")
	}
	last := at(11, 55).UnixMilli()
	if Allow(s, at(12, 0), Usage{LastBotCallMs: last}, Ask{Judgement: true}) == nil {
		t.Error("cool-down not enforced for judgement calls")
	}
	if err := Allow(s, at(12, 0), Usage{LastBotCallMs: last}, Ask{}); err != nil {
		t.Errorf("rules skip cool-down: %v", err)
	}
	if Allow(s, at(12, 0), Usage{InCall: true}, Ask{}) == nil {
		t.Error("call in progress not refused")
	}
}

func TestCheckOpener(t *testing.T) {
	good := "Hey, Atlas here. The migration task failed on the auth tests."
	if got, ok := CheckOpener("Atlas", good); !ok || got != good {
		t.Fatalf("good opener rejected: %q", got)
	}
	for _, bad := range []string{
		"Hi, it's your assistant.",                     // no name
		"Atlas from Orole here, task failed.",          // app name
		"Atlas here, " + strings.Repeat("x", 240),      // too long
		"Atlas here.\n\nSecond paragraph.",             // multi paragraph
		"Atlasian bots say hi.",                        // name not on a word boundary
	} {
		if _, ok := CheckOpener("Atlas", bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	for i := 0; i < 6; i++ {
		f := FallbackOpener("Atlas", "the deploy finished", i)
		if _, ok := CheckOpener("Atlas", f); !ok {
			t.Errorf("fallback %d fails its own check: %q", i, f)
		}
	}
}
```

Add at the bottom of the test file:

```go
func errorsAs(err error, target any) bool { return errors.As(err, target) }
```

and `"errors"` to the imports.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/jarvis/ -run 'TestCheckGate|TestAllowPolicy|TestCheckOpener'`
Expected: FAIL, undefined `CheckGate`, `Allow`, `CheckOpener`.

- [ ] **Step 3: Implement `gate.go`**

```go
package jarvis

// GateInput is the bot's effective permission state; the daemon resolves global fallbacks before calling CheckGate.
type GateInput struct {
	ToolAccess   string // effective: bot override or global (chat.ToolAccess*)
	TaskMode     string // effective code-task permission mode: manual | auto | yolo
	BotYolo      bool   // agents.botYolo
	YoloDisabled bool   // agents.yoloDisabled
	CodeTasks    bool   // BotConfig.CodeTasks
}

type GateError struct{ Reason string }

func (e *GateError) Error() string { return e.Reason }

// CheckGate allows Jarvis only for bots that already act without asking: phone calls have no approval prompt.
func CheckGate(in GateInput) error {
	switch {
	case in.ToolAccess != "all":
		return &GateError{"Jarvis needs this bot's tool access set to All."}
	case !in.CodeTasks:
		return &GateError{"Jarvis needs code tasks turned on for this bot."}
	case in.TaskMode != "yolo":
		return &GateError{"Jarvis needs this bot's code tasks set to YOLO."}
	case in.YoloDisabled:
		return &GateError{"Jarvis needs YOLO mode allowed in Task permissions."}
	case !in.BotYolo:
		return &GateError{"Jarvis needs \"Bots may start YOLO tasks\" turned on in Bot code tasks."}
	}
	return nil
}
```

- [ ] **Step 4: Implement `policy.go`**

```go
package jarvis

import (
	"fmt"
	"time"
)

const coolDown = 10 * time.Minute

type Usage struct {
	Today           int   // calls placed since local midnight
	MonthCostMicros int64 // estimated cost since the 1st of the month (1 USD = 1_000_000)
	LastBotCallMs   int64 // last outbound call of this bot
	InCall          bool
}

type Ask struct {
	Judgement     bool // call_owner (not a rule): the cool-down applies
	QuietOverride bool
}

type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

func Allow(s Settings, now time.Time, u Usage, a Ask) error {
	if u.InCall {
		return &Refusal{"A call is already in progress."}
	}
	if !a.QuietOverride && InQuietHours(s, now) {
		return &Refusal{"It is quiet hours."}
	}
	if u.Today >= s.dailyCap() {
		return &Refusal{fmt.Sprintf("The daily limit of %d calls is reached.", s.dailyCap())}
	}
	if u.MonthCostMicros >= int64(s.monthlyCapCents())*10_000 {
		return &Refusal{fmt.Sprintf("The monthly cost limit of $%.2f is reached.", float64(s.monthlyCapCents())/100)}
	}
	if a.Judgement && u.LastBotCallMs > 0 && now.Sub(time.UnixMilli(u.LastBotCallMs)) < coolDown {
		return &Refusal{"This bot called less than 10 minutes ago."}
	}
	return nil
}

func minutes(hhmmStr string) (int, bool) {
	if !hhmm.MatchString(hhmmStr) {
		return 0, false
	}
	return int(hhmmStr[0]-'0')*600 + int(hhmmStr[1]-'0')*60 + int(hhmmStr[3]-'0')*10 + int(hhmmStr[4]-'0'), true
}

// InQuietHours reports whether now (in its own location) falls in [QuietStart, QuietEnd), wrapping midnight.
func InQuietHours(s Settings, now time.Time) bool {
	start, ok1 := minutes(s.QuietStart)
	end, ok2 := minutes(s.QuietEnd)
	if !ok1 || !ok2 || start == end {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}
```

- [ ] **Step 5: Implement `opener.go`**

```go
package jarvis

import (
	"fmt"
	"regexp"
	"strings"
)

const maxOpener = 240

var appName = regexp.MustCompile(`(?i)orole`)

// CheckOpener accepts a model-written first line only if it names the bot, never names the app, and is one short paragraph.
func CheckOpener(name, text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxOpener || strings.Contains(text, "\n") || appName.MatchString(text) {
		return "", false
	}
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	if !re.MatchString(text) {
		return "", false
	}
	return text, true
}

var fallbacks = []string{
	"%s here, quick one: %s.",
	"Hey, it's %s. %s.",
	"Hi, %s calling. %s.",
	"%s here. Heads up: %s.",
	"It's %s. %s.",
	"Hey, %s here. %s.",
}

// FallbackOpener is used when the model's opener is late or fails CheckOpener.
func FallbackOpener(name, reason string, seed int) string {
	reason = strings.TrimSuffix(strings.TrimSpace(reason), ".")
	if reason == "" {
		reason = "I have an update for you"
	}
	if len(reason) > 160 {
		reason = reason[:160]
	}
	reason = appName.ReplaceAllString(reason, "the app")
	f := fallbacks[((seed%len(fallbacks))+len(fallbacks))%len(fallbacks)]
	if strings.HasPrefix(f, "%s") {
		return fmt.Sprintf(f, name, reason)
	}
	return fmt.Sprintf(f, name, strings.ToUpper(reason[:1])+reason[1:])
}
```

- [ ] **Step 6: Run the tests until they pass**

Run: `go test ./internal/jarvis/ -race -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/jarvis/
git commit -m "feat(jarvis): add permission gate, call policy and opener check"
```

---

### Task 3: Rule matching and the at_time scheduler

**Files:**
- Create: `internal/jarvis/rules.go`, `internal/jarvis/rules_test.go`

**Interfaces:**
- Consumes: `Rule`, `Trigger`, `hhmm` (Task 1).
- Produces:
  - `type Event struct{Trigger Trigger; OwnerBotID, TargetID, Source, Title, Detail string}` (for notifications `TargetID` = notification kind, `Source` = notification source)
  - `func Matches(r Rule, ev Event, nowMs int64) bool`
  - `func NextAt(r Rule, after time.Time) time.Time` (zero if not an `at_time` rule)
  - `func Expired(r Rule, nowMs int64) bool`

- [ ] **Step 1: Write the failing test**

```go
package jarvis

import (
	"testing"
	"time"
)

func TestMatches(t *testing.T) {
	now := int64(5_000)
	cases := []struct {
		name string
		r    Rule
		ev   Event
		want bool
	}{
		{"specific task", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFinished, TargetID: "t1"}, true},
		{"other task", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFinished, TargetID: "t2"}, false},
		{"any own task", Rule{BotID: "b1", Trigger: TaskFailed, Target: "any"}, Event{Trigger: TaskFailed, TargetID: "t9", OwnerBotID: "b1"}, true},
		{"any task of another bot", Rule{BotID: "b1", Trigger: TaskFailed, Target: "any"}, Event{Trigger: TaskFailed, TargetID: "t9", OwnerBotID: "b2"}, false},
		{"any automation", Rule{BotID: "b1", Trigger: AutomationFailed, Target: "any"}, Event{Trigger: AutomationFailed, TargetID: "a1"}, true},
		{"wrong trigger", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFailed, TargetID: "t1"}, false},
		{"notification kind", Rule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, true},
		{"notification kind+source", Rule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:github"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, true},
		{"notification wrong source", Rule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:mail"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, false},
		{"expired", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1", ExpiresAt: 4_000}, Event{Trigger: TaskFinished, TargetID: "t1"}, false},
		{"at_time never matches events", Rule{BotID: "b1", Trigger: AtTime, Target: "08:00"}, Event{Trigger: AtTime}, false},
	}
	for _, c := range cases {
		if got := Matches(c.r, c.ev, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestNextAt(t *testing.T) {
	loc := time.FixedZone("x", 2*3600)
	r := Rule{Trigger: AtTime, Target: "08:30"}
	before := time.Date(2026, 10, 14, 7, 0, 0, 0, loc)
	if got := NextAt(r, before); !got.Equal(time.Date(2026, 10, 14, 8, 30, 0, 0, loc)) {
		t.Errorf("same day: %v", got)
	}
	after := time.Date(2026, 10, 14, 9, 0, 0, 0, loc)
	if got := NextAt(r, after); !got.Equal(time.Date(2026, 10, 15, 8, 30, 0, 0, loc)) {
		t.Errorf("next day: %v", got)
	}
	if !NextAt(Rule{Trigger: TaskFinished, Target: "x"}, after).IsZero() {
		t.Error("non-time rule has a next time")
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/jarvis/ -run 'TestMatches|TestNextAt'`
Expected: FAIL, undefined `Matches`.

- [ ] **Step 3: Implement `rules.go`**

```go
package jarvis

import (
	"strings"
	"time"
)

// Event is something a rule can fire on. The daemon builds it from task, automation and notification hooks.
type Event struct {
	Trigger    Trigger
	OwnerBotID string // bot that started the task, if any
	TargetID   string // task id, automation id, or notification kind
	Source     string // notification source (e.g. "github.ci")
	Title      string // short human title for the brief
	Detail     string // untrusted detail (task result, error); fenced before it reaches a model
}

func Expired(r Rule, nowMs int64) bool { return r.ExpiresAt > 0 && nowMs >= r.ExpiresAt }

func Matches(r Rule, ev Event, nowMs int64) bool {
	if r.Trigger == AtTime || r.Trigger != ev.Trigger || Expired(r, nowMs) {
		return false
	}
	switch r.Trigger {
	case TaskFinished, TaskFailed:
		if r.Target == "any" {
			return ev.OwnerBotID == r.BotID
		}
		return r.Target == ev.TargetID
	case AutomationFinished, AutomationFailed:
		return r.Target == "any" || r.Target == ev.TargetID
	case NotificationTrigger:
		kind, source, _ := strings.Cut(r.Target, ":")
		if kind != ev.TargetID {
			return false
		}
		return source == "" || strings.HasPrefix(ev.Source, source)
	}
	return false
}

// NextAt is the next wall-clock occurrence of an at_time rule strictly after `after`, in after's location.
func NextAt(r Rule, after time.Time) time.Time {
	if r.Trigger != AtTime {
		return time.Time{}
	}
	m, ok := minutes(r.Target)
	if !ok {
		return time.Time{}
	}
	y, mo, d := after.Date()
	t := time.Date(y, mo, d, m/60, m%60, 0, 0, after.Location())
	if !t.After(after) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}
```

- [ ] **Step 4: Run the tests until they pass**

Run: `go test ./internal/jarvis/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jarvis/rules.go internal/jarvis/rules_test.go
git commit -m "feat(jarvis): match rules against task, automation and notification events"
```

---

### Task 4: Relay protocol and reconnecting client

**Files:**
- Create: `internal/jarvis/relay.go`, `internal/jarvis/relay_test.go`
- Modify: `go.mod`, `go.sum` (`go get github.com/coder/websocket@v1.8.13`)

**Interfaces:**
- Produces:
  - Frame types (constants) `FrameLLMRequest = "llm.request"`, `FrameLLMDelta = "llm.delta"`, `FrameLLMDone = "llm.done"`, `FrameLLMError = "llm.error"`, `FrameInboundLookup = "inbound.lookup"`, `FrameInboundAnswer = "inbound.answer"`, `FrameCallStatus = "call.status"`, `FrameHangup = "call.hangup"`
  - `type Frame struct{Type, ID, CallID string; Text string; Messages []LLMMessage; Vars map[string]string; Caller, Called, CallSID string; Accept bool; Status, ConversationID string; DurationSecs int; Overrides json.RawMessage}`
  - `type LLMMessage struct{Role, Content string}`
  - `type RelayClient struct` with `func NewRelayClient(url, token string, handle func(Frame), log *slog.Logger) *RelayClient`, `Run(ctx)` (blocks, reconnects with backoff 0.5 s → 10 s), `Send(ctx, Frame) error`, `Connected() bool`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/coder/websocket@v1.8.13`
Expected: `go.mod` gains `github.com/coder/websocket v1.8.13`.

- [ ] **Step 2: Write the failing test** (a fake relay with `httptest` + `websocket.Accept`)

```go
package jarvis

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func fakeRelay(t *testing.T, onConn func(c *websocket.Conn)) (*httptest.Server, *atomic.Int32) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conns.Add(1)
		onConn(c)
	}))
	t.Cleanup(srv.Close)
	return srv, &conns
}

func TestRelayClientRoundTripAndReconnect(t *testing.T) {
	got := make(chan Frame, 4)
	srv, conns := fakeRelay(t, func(c *websocket.Conn) {
		ctx := context.Background()
		b, _ := json.Marshal(Frame{Type: FrameLLMRequest, ID: "r1", CallID: "c1", Messages: []LLMMessage{{Role: "user", Content: "hi"}}})
		_ = c.Write(ctx, websocket.MessageText, b)
		_, data, err := c.Read(ctx)
		if err == nil {
			var f Frame
			_ = json.Unmarshal(data, &f)
			got <- f
		}
		c.Close(websocket.StatusGoingAway, "bye") // force a reconnect
	})
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	var rc *RelayClient
	rc = NewRelayClient(url, "tok", func(f Frame) {
		if f.Type == FrameLLMRequest {
			_ = rc.Send(context.Background(), Frame{Type: FrameLLMDelta, ID: f.ID, CallID: f.CallID, Text: "hello"})
		}
	}, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go rc.Run(ctx)
	for i := 0; i < 2; i++ {
		select {
		case f := <-got:
			if f.Type != FrameLLMDelta || f.Text != "hello" || f.ID != "r1" {
				t.Fatalf("frame %+v", f)
			}
		case <-ctx.Done():
			t.Fatal("timeout")
		}
	}
	if conns.Load() < 2 {
		t.Fatalf("did not reconnect: %d", conns.Load())
	}
}

func TestRelayClientSendWhileDisconnected(t *testing.T) {
	rc := NewRelayClient("ws://127.0.0.1:1", "tok", func(Frame) {}, slog.Default())
	if rc.Connected() {
		t.Fatal("connected without Run")
	}
	if err := rc.Send(context.Background(), Frame{Type: FrameHangup}); err != ErrRelayDown {
		t.Fatalf("err=%v", err)
	}
}
```

- [ ] **Step 3: Run it and see it fail**

Run: `go test ./internal/jarvis/ -run TestRelayClient`
Expected: FAIL, undefined `NewRelayClient`.

- [ ] **Step 4: Implement `relay.go`**

```go
package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	FrameLLMRequest    = "llm.request"    // relay -> daemon: a custom-LLM turn
	FrameLLMDelta      = "llm.delta"      // daemon -> relay: streamed text
	FrameLLMDone       = "llm.done"       // daemon -> relay: turn finished
	FrameLLMError      = "llm.error"      // daemon -> relay: turn failed (Text is spoken)
	FrameInboundLookup = "inbound.lookup" // relay -> daemon: inbound call webhook
	FrameInboundAnswer = "inbound.answer" // daemon -> relay: accept/reject + variables
	FrameCallStatus    = "call.status"    // relay -> daemon: post-call webhook
	FrameHangup        = "call.hangup"    // daemon -> relay: end the call after the current turn
)

var ErrRelayDown = errors.New("jarvis: relay not connected")

type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Frame struct {
	Type           string            `json:"type"`
	ID             string            `json:"id,omitempty"`     // request id, echoed in replies
	CallID         string            `json:"callId,omitempty"` // our call id (ElevenLabs dynamic variable call_id)
	Text           string            `json:"text,omitempty"`
	Messages       []LLMMessage      `json:"messages,omitempty"`
	Vars           map[string]string `json:"vars,omitempty"`
	Caller         string            `json:"caller,omitempty"`
	Called         string            `json:"called,omitempty"`
	CallSID        string            `json:"callSid,omitempty"`
	Accept         bool              `json:"accept,omitempty"`
	Status         string            `json:"status,omitempty"`
	ConversationID string            `json:"conversationId,omitempty"`
	DurationSecs   int               `json:"durationSecs,omitempty"`
	Overrides      json.RawMessage   `json:"overrides,omitempty"`
}

// RelayClient keeps one authenticated WebSocket to the relay open and hands every received frame to handle.
// handle runs on its own goroutine per frame so a slow model turn never blocks the read loop.
type RelayClient struct {
	url, token string
	handle     func(Frame)
	log        *slog.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

func NewRelayClient(url, token string, handle func(Frame), log *slog.Logger) *RelayClient {
	return &RelayClient{url: url, token: token, handle: handle, log: log}
}

func (c *RelayClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *RelayClient) Send(ctx context.Context, f Frame) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return ErrRelayDown
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (c *RelayClient) Run(ctx context.Context) {
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		start := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		c.log.Warn("jarvis: relay connection ended", "err", err)
		if time.Since(start) > 30*time.Second {
			backoff = 500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}

func (c *RelayClient) session(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dctx, c.url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}}})
	cancel()
	if err != nil {
		return err
	}
	conn.SetReadLimit(1 << 20)
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		conn.CloseNow()
	}()
	pctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-t.C:
				p, cancel := context.WithTimeout(pctx, 5*time.Second)
				err := conn.Ping(p)
				cancel()
				if err != nil {
					conn.CloseNow()
					return
				}
			}
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var f Frame
		if json.Unmarshal(data, &f) != nil || f.Type == "" {
			continue
		}
		go c.handle(f)
	}
}
```

- [ ] **Step 5: Run the tests until they pass**

Run: `go test ./internal/jarvis/ -race -run TestRelayClient -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/jarvis/relay.go internal/jarvis/relay_test.go
git commit -m "feat(jarvis): add reconnecting relay websocket client"
```

---

### Task 5: Cloudflare relay worker

**Files:**
- Create: `relay/package.json`, `relay/wrangler.toml`, `relay/tsconfig.json`, `relay/vitest.config.ts`, `relay/src/index.ts`, `relay/src/hub.ts`, `relay/test/relay.test.ts`, `relay/README.md`

**Interfaces:**
- Consumes: the frame protocol from Task 4 (JSON field names must match exactly).
- Produces HTTP endpoints:
  - `GET /daemon` (WebSocket upgrade, `Authorization: Bearer <RELAY_TOKEN>`)
  - `POST /llm/chat/completions` (ElevenLabs custom LLM; header `x-orole-relay: <LLM_SECRET>`; OpenAI-style SSE response)
  - `POST /inbound` (ElevenLabs inbound personalization webhook; same header secret; returns `{type:"conversation_initiation_client_data", dynamic_variables, conversation_config_override}` or 403)
  - `POST /status` (ElevenLabs post-call webhook; `ElevenLabs-Signature: t=<ts>,v0=<hex hmac_sha256(WEBHOOK_SECRET, "<ts>.<body>")>`)
  - `GET /health` → `{daemon: boolean}` (no secrets)
- Worker secrets: `RELAY_TOKEN`, `LLM_SECRET`, `WEBHOOK_SECRET`.

- [ ] **Step 1: Scaffold**

`relay/package.json`:

```json
{
  "name": "orole-jarvis-relay",
  "private": true,
  "type": "module",
  "scripts": { "test": "vitest run", "deploy": "wrangler deploy", "typecheck": "tsc --noEmit" },
  "devDependencies": {
    "@cloudflare/vitest-pool-workers": "0.8.71",
    "@cloudflare/workers-types": "4.20250913.0",
    "typescript": "5.9.2",
    "vitest": "3.2.4",
    "wrangler": "4.37.0"
  }
}
```

(Use the newest versions that are at least two weeks old when you start; keep them pinned exactly.)

`relay/wrangler.toml`:

```toml
name = "orole-jarvis-relay"
main = "src/index.ts"
compatibility_date = "2026-09-01"

[[durable_objects.bindings]]
name = "HUB"
class_name = "Hub"

[[migrations]]
tag = "v1"
new_sqlite_classes = ["Hub"]
```

`relay/vitest.config.ts`:

```ts
import { defineWorkersConfig } from "@cloudflare/vitest-pool-workers/config";

export default defineWorkersConfig({
  test: {
    poolOptions: {
      workers: {
        wrangler: { configPath: "./wrangler.toml" },
        miniflare: { bindings: { RELAY_TOKEN: "rt", LLM_SECRET: "ls", WEBHOOK_SECRET: "ws" } },
      },
    },
  },
});
```

Run: `bun install --cwd relay`

- [ ] **Step 2: Write the failing tests**

```ts
import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

async function daemon() {
  const res = await SELF.fetch("https://relay/daemon", { headers: { Upgrade: "websocket", Authorization: "Bearer rt" } });
  expect(res.status).toBe(101);
  const ws = res.webSocket!;
  ws.accept();
  return ws;
}

const llmBody = JSON.stringify({
  model: "bot",
  stream: true,
  messages: [{ role: "user", content: "what's running?" }],
  elevenlabs_extra_body: { call_id: "c1" },
});

describe("auth", () => {
  it("rejects a daemon without the token", async () => {
    const res = await SELF.fetch("https://relay/daemon", { headers: { Upgrade: "websocket", Authorization: "Bearer nope" } });
    expect(res.status).toBe(401);
  });
  it("rejects llm calls without the secret header", async () => {
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", body: llmBody });
    expect(res.status).toBe(401);
  });
  it("rejects post-call webhooks with a bad signature", async () => {
    const res = await SELF.fetch("https://relay/status", { method: "POST", headers: { "ElevenLabs-Signature": "t=1,v0=00" }, body: "{}" });
    expect(res.status).toBe(401);
  });
});

describe("llm", () => {
  it("speaks a goodbye when no daemon is connected", async () => {
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    expect(res.headers.get("content-type")).toContain("text/event-stream");
    const text = await res.text();
    expect(text).toContain("can't reach your bots right now");
    expect(text).toContain("data: [DONE]");
    expect(text.toLowerCase()).not.toContain("orole");
  });

  it("forwards a turn to the daemon and streams its deltas as SSE", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type !== "llm.request") return;
      expect(f.callId).toBe("c1");
      expect(f.messages.at(-1).content).toBe("what's running?");
      ws.send(JSON.stringify({ type: "llm.delta", id: f.id, text: "Two tasks " }));
      ws.send(JSON.stringify({ type: "llm.delta", id: f.id, text: "are running." }));
      ws.send(JSON.stringify({ type: "llm.done", id: f.id }));
    });
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    const text = await res.text();
    expect(text).toContain('"content":"Two tasks "');
    expect(text).toContain('"content":"are running."');
    expect(text.trim().endsWith("data: [DONE]")).toBe(true);
    ws.close();
  });
});

describe("inbound", () => {
  it("returns the daemon's answer as conversation initiation data", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type === "inbound.lookup") ws.send(JSON.stringify({ type: "inbound.answer", id: f.id, accept: true, vars: { call_id: "c9", bot_name: "Atlas" } }));
    });
    const res = await SELF.fetch("https://relay/inbound", {
      method: "POST", headers: { "x-orole-relay": "ls" },
      body: JSON.stringify({ caller_id: "+32470000000", called_number: "+3220000000", call_sid: "CA1", agent_id: "ag" }),
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as { type: string; dynamic_variables: Record<string, string> };
    expect(body.type).toBe("conversation_initiation_client_data");
    expect(body.dynamic_variables.call_id).toBe("c9");
    ws.close();
  });
  it("rejects when the daemon refuses", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type === "inbound.lookup") ws.send(JSON.stringify({ type: "inbound.answer", id: f.id, accept: false }));
    });
    const res = await SELF.fetch("https://relay/inbound", { method: "POST", headers: { "x-orole-relay": "ls" }, body: JSON.stringify({ caller_id: "+1" }) });
    expect(res.status).toBe(403);
    ws.close();
  });
});
```

- [ ] **Step 3: Run them and see them fail**

Run: `bun run --cwd relay test`
Expected: FAIL (no `src/index.ts`).

- [ ] **Step 4: Implement `relay/src/index.ts`**

```ts
export { Hub } from "./hub";

export interface Env {
  HUB: DurableObjectNamespace;
  RELAY_TOKEN: string;
  LLM_SECRET: string;
  WEBHOOK_SECRET: string;
}

function safeEqual(a: string, b: string): boolean {
  const x = new TextEncoder().encode(a);
  const y = new TextEncoder().encode(b);
  if (x.length !== y.length) return false;
  let d = 0;
  for (let i = 0; i < x.length; i++) d |= x[i] ^ y[i];
  return d === 0;
}

async function hmacHex(secret: string, data: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const sig = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(data));
  return [...new Uint8Array(sig)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function validSignature(header: string | null, body: string, secret: string): Promise<boolean> {
  if (!header) return false;
  const parts = Object.fromEntries(header.split(",").map((p) => p.split("=", 2) as [string, string]));
  const ts = Number(parts.t);
  if (!parts.v0 || !Number.isFinite(ts) || Math.abs(Date.now() / 1000 - ts) > 30 * 60) return false;
  return safeEqual(await hmacHex(secret, `${parts.t}.${body}`), parts.v0);
}

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);
    const hub = env.HUB.get(env.HUB.idFromName("hub"));
    switch (url.pathname) {
      case "/health":
        return hub.fetch(req);
      case "/daemon":
        if (!safeEqual(req.headers.get("Authorization") ?? "", `Bearer ${env.RELAY_TOKEN}`)) return new Response("unauthorized", { status: 401 });
        return hub.fetch(req);
      case "/llm/chat/completions":
      case "/inbound":
        if (req.method !== "POST" || !safeEqual(req.headers.get("x-orole-relay") ?? "", env.LLM_SECRET)) return new Response("unauthorized", { status: 401 });
        return hub.fetch(req);
      case "/status": {
        const body = await req.text();
        if (!(await validSignature(req.headers.get("ElevenLabs-Signature"), body, env.WEBHOOK_SECRET))) return new Response("unauthorized", { status: 401 });
        return hub.fetch(new Request(req.url, { method: "POST", body }));
      }
    }
    return new Response("not found", { status: 404 });
  },
};
```

- [ ] **Step 5: Implement `relay/src/hub.ts`**

```ts
import { DurableObject } from "cloudflare:workers";

type Frame = { type: string; id?: string; callId?: string; text?: string; accept?: boolean; vars?: Record<string, string>; overrides?: unknown; [k: string]: unknown };
type Pending = { onFrame: (f: Frame) => void };

const GOODBYE = "Sorry, I can't reach your bots right now. I'll message you instead.";
const LOST = "Lost connection, I'll message you.";
const enc = new TextEncoder();

function sseChunk(content: string): Uint8Array {
  return enc.encode(`data: ${JSON.stringify({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta: { content }, finish_reason: null }] })}\n\n`);
}
const sseDone = enc.encode(`data: ${JSON.stringify({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`);

export class Hub extends DurableObject {
  private daemon: WebSocket | null = null;
  private pending = new Map<string, Pending>();

  async fetch(req: Request): Promise<Response> {
    const path = new URL(req.url).pathname;
    if (path === "/health") return Response.json({ daemon: this.daemon !== null });
    if (path === "/daemon") return this.attach();
    if (path === "/llm/chat/completions") return this.llm(await req.json());
    if (path === "/inbound") return this.inbound(await req.json());
    if (path === "/status") {
      this.send({ type: "call.status", ...(this.statusFrame(await req.json())) });
      return new Response("ok");
    }
    return new Response("not found", { status: 404 });
  }

  private attach(): Response {
    const pair = new WebSocketPair();
    const [client, server] = [pair[0], pair[1]];
    server.accept();
    this.daemon?.close(4000, "replaced");
    this.daemon = server;
    server.addEventListener("message", (e) => {
      let f: Frame;
      try { f = JSON.parse(e.data as string); } catch { return; }
      if (f.id) this.pending.get(f.id)?.onFrame(f);
    });
    const gone = () => { if (this.daemon === server) this.daemon = null; };
    server.addEventListener("close", gone);
    server.addEventListener("error", gone);
    return new Response(null, { status: 101, webSocket: client });
  }

  private send(f: Frame): boolean {
    if (!this.daemon) return false;
    try { this.daemon.send(JSON.stringify(f)); return true; } catch { return false; }
  }

  private llm(body: { messages?: { role: string; content: string }[]; elevenlabs_extra_body?: Record<string, string> }): Response {
    const { readable, writable } = new TransformStream<Uint8Array>();
    const w = writable.getWriter();
    const headers = { "content-type": "text/event-stream", "cache-control": "no-cache" };
    const id = crypto.randomUUID();
    const callId = body.elevenlabs_extra_body?.call_id ?? "";
    const finish = async (spoken?: string) => {
      this.pending.delete(id);
      if (spoken) await w.write(sseChunk(spoken));
      await w.write(sseDone);
      await w.close();
    };
    const sent = this.send({ type: "llm.request", id, callId, messages: (body.messages ?? []).map(({ role, content }) => ({ role, content })) });
    if (!sent) {
      void finish(GOODBYE);
      return new Response(readable, { headers });
    }
    // If the daemon socket drops mid-turn, wait 5 s for it to come back, then give up.
    let timer: ReturnType<typeof setTimeout> | undefined;
    const arm = () => { clearTimeout(timer); timer = setTimeout(() => { if (!this.daemon) void finish(LOST); else arm(); }, 5000); };
    arm();
    this.pending.set(id, {
      onFrame: (f) => {
        arm();
        if (f.type === "llm.delta" && f.text) void w.write(sseChunk(f.text));
        else if (f.type === "llm.done") { clearTimeout(timer); void finish(); }
        else if (f.type === "llm.error") { clearTimeout(timer); void finish(f.text || "That failed."); }
      },
    });
    return new Response(readable, { headers });
  }

  private async inbound(body: { caller_id?: string; called_number?: string; call_sid?: string }): Promise<Response> {
    const id = crypto.randomUUID();
    const answer = new Promise<Frame | null>((resolve) => {
      const t = setTimeout(() => { this.pending.delete(id); resolve(null); }, 4000);
      this.pending.set(id, { onFrame: (f) => { if (f.type === "inbound.answer") { clearTimeout(t); this.pending.delete(id); resolve(f); } } });
    });
    if (!this.send({ type: "inbound.lookup", id, caller: body.caller_id ?? "", called: body.called_number ?? "", callSid: body.call_sid ?? "" })) {
      this.pending.delete(id);
      return new Response("daemon offline", { status: 503 });
    }
    const f = await answer;
    if (!f || !f.accept) return new Response("rejected", { status: 403 });
    return Response.json({ type: "conversation_initiation_client_data", dynamic_variables: f.vars ?? {}, conversation_config_override: f.overrides ?? {} });
  }

  private statusFrame(body: { data?: { conversation_id?: string; status?: string; metadata?: { call_duration_secs?: number }; conversation_initiation_client_data?: { dynamic_variables?: Record<string, string> } } }): Frame {
    const d = body.data ?? {};
    return {
      type: "call.status",
      callId: d.conversation_initiation_client_data?.dynamic_variables?.call_id ?? "",
      conversationId: d.conversation_id ?? "",
      status: d.status ?? "",
      durationSecs: d.metadata?.call_duration_secs ?? 0,
    };
  }
}
```

- [ ] **Step 6: Run the tests until they pass**

Run: `bun run --cwd relay test && bun run --cwd relay typecheck`
Expected: PASS (7 tests), no type errors.

- [ ] **Step 7: Write `relay/README.md`**

Contents: what the relay is (a stateless forwarder; stores nothing), the endpoints table above, and deployment:

```
bunx wrangler@4 deploy                      # from relay/
bunx wrangler@4 secret put RELAY_TOKEN      # paste the value shown in Settings › Phone
bunx wrangler@4 secret put LLM_SECRET
bunx wrangler@4 secret put WEBHOOK_SECRET
```

Note that the three values are generated by the app and shown once in Settings › Phone with a copy button.

- [ ] **Step 8: Commit**

```bash
git add relay/
git commit -m "feat(relay): add cloudflare relay for jarvis calls"
```

---

### Task 6: Provider clients and provisioning

**Files:**
- Create: `internal/jarvis/elevenlabs.go`, `internal/jarvis/twilio.go`, `internal/jarvis/provision.go`, `internal/jarvis/providers_test.go`

**Interfaces:**
- Produces:
  - `type ElevenLabs struct{Key string; BaseURL string; HTTP *http.Client}` (BaseURL default `https://api.elevenlabs.io`)
    - `Voices(ctx) ([]Voice, error)`; `type Voice struct{ID, Name string}`
    - `Models(ctx) ([]TTSModel, error)`; `type TTSModel struct{ID, Name string; Latency string}` (latency hint "fast" for ids containing `flash` or `turbo`, else "expressive")
    - `CreateAgent(ctx, AgentConfig) (string, error)`, `UpdateAgent(ctx, id string, AgentConfig) error`
    - `ImportTwilioNumber(ctx, number, label, sid, token string) (string, error)`, `AssignAgent(ctx, phoneNumberID, agentID string) error`
    - `OutboundCall(ctx, OutboundCall) (conversationID, callSID string, err error)`
    - `ConversationStatus(ctx, id string) (status string, durationSecs int, err error)`
  - `type AgentConfig struct{RelayURL, LLMSecret, InboundURL, WebhookURL, VoiceID, TTSModel string}`
  - `type OutboundCall struct{AgentID, PhoneNumberID, To, CallID, BotID, VoiceID, TTSModel string}`
  - `type Twilio struct{SID, Token, BaseURL string; HTTP *http.Client}`: `Numbers(ctx) ([]string, error)` (E.164 of incoming numbers; also validates credentials)
  - `func Provision(ctx, el *ElevenLabs, tw *Twilio, s Settings, secrets ProvisionSecrets) (Settings, error)`; `type ProvisionSecrets struct{LLMSecret, TwilioSID, TwilioToken string}`

- [ ] **Step 1: Verify the ElevenLabs request shapes against current docs**

Open (in the browser pane) and read, before writing code:
- `https://elevenlabs.io/docs/api-reference/agents/create` and `.../agents/update`
- `https://elevenlabs.io/docs/eleven-agents/customization/llm/custom-llm`
- `https://elevenlabs.io/docs/api-reference/phone-numbers/create` (Twilio import)
- `https://elevenlabs.io/docs/api-reference/twilio/outbound-call`
- `https://elevenlabs.io/docs/eleven-agents/customization/personalization/twilio-personalization`
- `https://elevenlabs.io/docs/eleven-agents/workflows/post-call-webhooks`
- `https://elevenlabs.io/docs/eleven-agents/customization/tools/system-tools/voicemail-detection`

Record any field-name differences from the shapes below directly in the struct tags you write in Step 3. The shapes below are the expected 2026 API; the tests pin whatever you write, so the fake server in Step 2 must use the same field names.

- [ ] **Step 2: Write the failing tests** against an `httptest` fake that records requests

```go
package jarvis

import (
	"context"
	"encoding/json"
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
	for _, want := range []string{`"to_number":"+32470000000"`, `"call_id":"c1"`, `"bot_id":"b1"`, `"voice_id":"v1"`, `"model_id":"eleven_flash_v2_5"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("body lacks %s: %s", want, b)
		}
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

func TestProvisionCreatesAgentAndImportsNumber(t *testing.T) {
	srv, reqs := fakeAPI(t, map[string]string{
		"POST /v1/convai/agents/create":  `{"agent_id":"ag1"}`,
		"POST /v1/convai/phone-numbers":  `{"phone_number_id":"pn1"}`,
		"PATCH /v1/convai/phone-numbers/pn1": `{}`,
		"GET /2010-04-01/Accounts/AC1/IncomingPhoneNumbers.json": `{"incoming_phone_numbers":[{"phone_number":"+3220000000"}]}`,
	})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	s, err := Provision(context.Background(), el, tw, Settings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000"}, ProvisionSecrets{LLMSecret: "ls", TwilioSID: "AC1", TwilioToken: "t"})
	if err != nil || s.AgentID != "ag1" || s.PhoneNumberID != "pn1" {
		t.Fatalf("s=%+v err=%v", s, err)
	}
	var create recorded
	for _, r := range reqs() {
		if r.Path == "/v1/convai/agents/create" {
			create = r
		}
	}
	b, _ := json.Marshal(create.Body)
	for _, want := range []string{`"url":"https://relay.example/llm"`, `"x-orole-relay"`, `"first_message":""`, `voicemail_detection`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("agent config lacks %s: %s", want, b)
		}
	}
	if strings.Contains(strings.ToLower(string(b)), "orole os") {
		t.Error("agent config names the app")
	}
}

func TestProvisionRejectsNumberNotInTwilio(t *testing.T) {
	srv, _ := fakeAPI(t, map[string]string{"GET /2010-04-01/Accounts/AC1/IncomingPhoneNumbers.json": `{"incoming_phone_numbers":[]}`})
	el := &ElevenLabs{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	tw := &Twilio{SID: "AC1", Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := Provision(context.Background(), el, tw, Settings{RelayURL: "https://r", TwilioNumber: "+1"}, ProvisionSecrets{}); err == nil {
		t.Fatal("accepted a number Twilio does not own")
	}
}
```

- [ ] **Step 3: Run them and see them fail**

Run: `go test ./internal/jarvis/ -run 'TestElevenLabs|TestModels|TestProvision'`
Expected: FAIL, undefined `ElevenLabs`.

- [ ] **Step 4: Implement `elevenlabs.go`**

```go
package jarvis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ElevenLabs struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
}

type Voice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TTSModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Latency string `json:"latency"` // "fast" | "expressive"
}

type AgentConfig struct {
	RelayURL, LLMSecret, VoiceID, TTSModel string
}

type OutboundCall struct {
	AgentID, PhoneNumberID, To, CallID, BotID, VoiceID, TTSModel string
}

func (e *ElevenLabs) base() string {
	if e.BaseURL != "" {
		return strings.TrimRight(e.BaseURL, "/")
	}
	return "https://api.elevenlabs.io"
}

func (e *ElevenLabs) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("xi-api-key", e.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := e.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("ElevenLabs %s %s: %d %s", method, path, res.StatusCode, strings.TrimSpace(string(data[:min(len(data), 300)])))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (e *ElevenLabs) Voices(ctx context.Context) ([]Voice, error) {
	var res struct {
		Voices []struct {
			VoiceID string `json:"voice_id"`
			Name    string `json:"name"`
		} `json:"voices"`
	}
	if err := e.do(ctx, "GET", "/v1/voices", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Voice, 0, len(res.Voices))
	for _, v := range res.Voices {
		out = append(out, Voice{ID: v.VoiceID, Name: v.Name})
	}
	return out, nil
}

func (e *ElevenLabs) Models(ctx context.Context) ([]TTSModel, error) {
	var res []struct {
		ModelID string `json:"model_id"`
		Name    string `json:"name"`
		TTS     bool   `json:"can_do_text_to_speech"`
	}
	if err := e.do(ctx, "GET", "/v1/models", nil, &res); err != nil {
		return nil, err
	}
	var out []TTSModel
	for _, m := range res {
		if !m.TTS {
			continue
		}
		lat := "expressive"
		if strings.Contains(m.ModelID, "flash") || strings.Contains(m.ModelID, "turbo") {
			lat = "fast"
		}
		out = append(out, TTSModel{ID: m.ModelID, Name: m.Name, Latency: lat})
	}
	return out, nil
}

// agentBody configures the shared agent: the brain is our relay (custom LLM), the first turn comes from the LLM
// (first_message empty) so the daemon's cached opener is spoken, and voicemail detection ends unanswered calls.
func agentBody(c AgentConfig) map[string]any {
	return map[string]any{
		"name": "Jarvis",
		"conversation_config": map[string]any{
			"agent": map[string]any{
				"first_message": "",
				"language":      "en",
				"prompt": map[string]any{
					"prompt": "You relay a phone call. Speak the assistant's replies exactly.",
					"llm":    "custom-llm",
					"custom_llm": map[string]any{
						"url":             strings.TrimRight(c.RelayURL, "/") + "/llm",
						"model_id":        "bot",
						"request_headers": map[string]string{"x-orole-relay": c.LLMSecret},
					},
					"tools": []map[string]any{
						{"type": "system", "name": "end_call"},
						{"type": "system", "name": "voicemail_detection", "params": map[string]any{"voicemail_message": ""}},
					},
				},
			},
			"tts": map[string]any{"voice_id": c.VoiceID, "model_id": c.TTSModel, "optimize_streaming_latency": 4},
			"turn": map[string]any{"turn_eagerness": "eager"},
		},
		"platform_settings": map[string]any{
			"overrides": map[string]any{
				"conversation_config_override": map[string]any{"tts": map[string]any{"voice_id": true, "model_id": true}},
				"custom_llm_extra_body":        true,
			},
		},
	}
}

func (e *ElevenLabs) CreateAgent(ctx context.Context, c AgentConfig) (string, error) {
	var res struct {
		AgentID string `json:"agent_id"`
	}
	err := e.do(ctx, "POST", "/v1/convai/agents/create", agentBody(c), &res)
	return res.AgentID, err
}

func (e *ElevenLabs) UpdateAgent(ctx context.Context, id string, c AgentConfig) error {
	return e.do(ctx, "PATCH", "/v1/convai/agents/"+id, agentBody(c), nil)
}

func (e *ElevenLabs) ImportTwilioNumber(ctx context.Context, number, label, sid, token string) (string, error) {
	var res struct {
		ID string `json:"phone_number_id"`
	}
	err := e.do(ctx, "POST", "/v1/convai/phone-numbers", map[string]any{
		"provider": "twilio", "phone_number": number, "label": label, "sid": sid, "token": token,
	}, &res)
	return res.ID, err
}

func (e *ElevenLabs) AssignAgent(ctx context.Context, phoneNumberID, agentID string) error {
	return e.do(ctx, "PATCH", "/v1/convai/phone-numbers/"+phoneNumberID, map[string]any{"agent_id": agentID}, nil)
}

// SetWebhooks points the workspace's inbound personalization and post-call webhooks at the relay.
func (e *ElevenLabs) SetWebhooks(ctx context.Context, relayURL, llmSecret string) error {
	base := strings.TrimRight(relayURL, "/")
	return e.do(ctx, "PATCH", "/v1/convai/settings", map[string]any{
		"conversation_initiation_client_data_webhook": map[string]any{"url": base + "/inbound", "request_headers": map[string]string{"x-orole-relay": llmSecret}},
		"webhooks": map[string]any{"post_call_webhook_url": base + "/status"},
	}, nil)
}

func (e *ElevenLabs) OutboundCall(ctx context.Context, c OutboundCall) (string, string, error) {
	tts := map[string]any{}
	if c.VoiceID != "" {
		tts["voice_id"] = c.VoiceID
	}
	if c.TTSModel != "" {
		tts["model_id"] = c.TTSModel
	}
	var res struct {
		ConversationID string `json:"conversation_id"`
		CallSID        string `json:"callSid"`
	}
	err := e.do(ctx, "POST", "/v1/convai/twilio/outbound-call", map[string]any{
		"agent_id": c.AgentID, "agent_phone_number_id": c.PhoneNumberID, "to_number": c.To,
		"conversation_initiation_client_data": map[string]any{
			"dynamic_variables":            map[string]string{"call_id": c.CallID, "bot_id": c.BotID},
			"conversation_config_override": map[string]any{"tts": tts},
			"custom_llm_extra_body":        map[string]string{"call_id": c.CallID},
		},
	}, &res)
	return res.ConversationID, res.CallSID, err
}

func (e *ElevenLabs) ConversationStatus(ctx context.Context, id string) (string, int, error) {
	var res struct {
		Status   string `json:"status"`
		Metadata struct {
			Duration int `json:"call_duration_secs"`
		} `json:"metadata"`
	}
	err := e.do(ctx, "GET", "/v1/convai/conversations/"+id, nil, &res)
	return res.Status, res.Metadata.Duration, err
}
```

The relay reads the call id from `elevenlabs_extra_body.call_id` (Task 5); `custom_llm_extra_body` above is what ElevenLabs forwards there. If the docs name it differently, change both the Go map key and `hub.ts` in the same commit.

- [ ] **Step 5: Implement `twilio.go`**

```go
package jarvis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Twilio struct {
	SID, Token, BaseURL string
	HTTP                *http.Client
}

// Numbers lists the account's incoming numbers; a 401 means the SID or token is wrong.
func (t *Twilio) Numbers(ctx context.Context) ([]string, error) {
	base := strings.TrimRight(t.BaseURL, "/")
	if base == "" {
		base = "https://api.twilio.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/2010-04-01/Accounts/"+t.SID+"/IncomingPhoneNumbers.json", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(t.SID, t.Token)
	res, err := t.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("Twilio rejected the Account SID or auth token.")
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("Twilio returned %d.", res.StatusCode)
	}
	var body struct {
		Numbers []struct {
			PhoneNumber string `json:"phone_number"`
		} `json:"incoming_phone_numbers"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Numbers))
	for _, n := range body.Numbers {
		out = append(out, n.PhoneNumber)
	}
	return out, nil
}
```

- [ ] **Step 6: Implement `provision.go`**

```go
package jarvis

import (
	"context"
	"fmt"
	"slices"
)

type ProvisionSecrets struct{ LLMSecret, TwilioSID, TwilioToken string }

// Provision makes the ElevenLabs side match the settings: one agent pointing at the relay, the Twilio number
// imported and assigned to it, and the workspace webhooks pointing at the relay. Safe to run repeatedly.
func Provision(ctx context.Context, el *ElevenLabs, tw *Twilio, s Settings, sec ProvisionSecrets) (Settings, error) {
	nums, err := tw.Numbers(ctx)
	if err != nil {
		return s, err
	}
	if !slices.Contains(nums, s.TwilioNumber) {
		return s, fmt.Errorf("The number %s is not in this Twilio account.", s.TwilioNumber)
	}
	cfg := AgentConfig{RelayURL: s.RelayURL, LLMSecret: sec.LLMSecret, VoiceID: s.DefaultVoiceID, TTSModel: s.DefaultTTSModel}
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
	if err := el.SetWebhooks(ctx, s.RelayURL, sec.LLMSecret); err != nil {
		return s, err
	}
	return s, nil
}
```

Add `"PATCH /v1/convai/settings": "{}"` to the fake replies in `TestProvisionCreatesAgentAndImportsNumber`.

- [ ] **Step 7: Run the tests until they pass**

Run: `go test ./internal/jarvis/ -race -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/jarvis/
git commit -m "feat(jarvis): add elevenlabs and twilio clients and provisioning"
```

---

### Task 7: Chat engine voice mode

**Files:**
- Create: `internal/chat/voice.go`, `internal/chat/voice_test.go`
- Modify: `internal/chat/api.go` (BotJarvis, BotConfig.Jarvis, PartCall), `internal/chat/tool.go` (RunContext.CallID), `internal/chat/prompt.go` (voiceStyle, systemInput.Voice), `internal/chat/engine.go` (prep/turnEnv fields, consume, runTool, prepare), `internal/chat/store.go` (validation), `internal/chat/manager.go` (save check hook, ImportBot)

**Interfaces:**
- Produces:
  - `type BotJarvis struct{Enabled bool; Policy, VoiceID, TTSModel string}` (JSON `enabled,policy,voiceId,ttsModel`, all omitempty); `BotConfig.Jarvis BotJarvis \`json:"jarvis,omitzero"\``
  - `const PartCall = "call"` — uses `MsgPart` fields: `ID` (call id), `Name` (direction), `Status` (call status), `Title` (reason), `DurationMs`, `Summary` (e.g. "est. $0.24")
  - `RunContext.CallID string`
  - `type VoiceLine struct{Role, Text string}`
  - `type VoiceRequest struct{BotID, CallID, Text, Brief string; History []VoiceLine; OnText func(string); OnTool func(name string)}`
  - `func (m *Manager) VoiceTurn(ctx, VoiceRequest) (string, error)` — returns the full spoken reply
  - `func (m *Manager) Compose(ctx, botID, instruction string) (string, error)` — one model call, voice style, no tools, nothing stored
  - `func (m *Manager) PostCallCard(ctx, botID string, part MsgPart) (string, error)` — returns message id
  - `func (m *Manager) UpdateCallCard(ctx, msgID string, part MsgPart) error`
  - `func (m *Manager) SetSaveCheck(f func(ctx context.Context, in BotInput) error)`

- [ ] **Step 1: Add the types**

In `api.go` after `BotTaskDefaults`:

```go
// BotJarvis configures Jarvis mode: the bot may phone the owner and take their calls. Enabling it requires full
// permissions, checked by the daemon on save and before every call and action (see internal/jarvis).
type BotJarvis struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Policy   string `json:"policy,omitempty"`   // "When to call me", <= MaxJarvisPolicy
	VoiceID  string `json:"voiceId,omitempty"`  // ElevenLabs voice; "" = global default
	TTSModel string `json:"ttsModel,omitempty"` // ElevenLabs TTS model; "" = global default
}

const MaxJarvisPolicy = 2000
```

Add to `BotConfig` after `Tasks`:

```go
	// Jarvis lets the bot phone the owner and take their calls.
	Jarvis BotJarvis `json:"jarvis,omitzero"`
```

Add `PartCall = "call" // phone call card: ID call id, Name direction, Status, Title reason, DurationMs, Summary cost` to the Part constants. In `tool.go` add to `RunContext`: `CallID string // set while the bot speaks on a phone call`.

- [ ] **Step 2: Write the failing tests**

```go
package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

func TestVoiceTurnStreamsAndStores(t *testing.T) {
	e := newEnv(t, llmtest.Script{llmtest.Text("Two tasks are running.")})
	b := e.bot("Atlas")
	var mu sync.Mutex
	var spoken strings.Builder
	reply, err := e.m.VoiceTurn(context.Background(), VoiceRequest{
		BotID: b.ID, CallID: "c1", Text: "what's running?", Brief: "Reason: you asked me to call.",
		OnText: func(s string) { mu.Lock(); spoken.WriteString(s); mu.Unlock() },
	})
	if err != nil || reply != "Two tasks are running." {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if spoken.String() != reply {
		t.Fatalf("streamed %q", spoken.String())
	}
	h := e.history(b.ID)
	if len(h) != 2 || h[0].Role != "user" || replyText(h[1]) != reply {
		t.Fatalf("history %+v", h)
	}
}

func TestVoiceTurnUsesVoicePromptAndNoReasoning(t *testing.T) {
	e := newEnv(t, llmtest.Script{llmtest.Text("ok")})
	b := e.bot("Atlas", func(in *BotInput) { in.Reasoning = "high" })
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	req := e.llm.LastRequest()
	if req.Reasoning == "high" {
		t.Error("voice turn kept high reasoning")
	}
	if !strings.Contains(req.System, "phone call") || strings.Contains(req.System, chatStyle) {
		t.Error("voice turn did not swap chat style for voice style")
	}
}

func TestVoiceTurnRunsWhileChatTurnBusy(t *testing.T) {
	e := newEnv(t, llmtest.Script{llmtest.Block(), llmtest.Text("on the phone")})
	b := e.bot("Atlas")
	e.send(b.ID, "long job") // first script step blocks this chat turn
	reply, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hey"})
	if err != nil || reply != "on the phone" {
		t.Fatalf("voice turn waited behind chat turn: %q %v", reply, err)
	}
}

func TestVoiceTurnCallsOnToolBeforeRunning(t *testing.T) {
	ft := &fakeTool{id: "probe", specs: []llm.ToolSpec{{Name: "probe"}}}
	e := newEnv(t, llmtest.Script{llmtest.Call("probe", `{}`), llmtest.Text("done")}, func(c *Config) {})
	e.m.AddTools(ft)
	b := e.bot("Atlas")
	var tools []string
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "go", OnTool: func(n string) { tools = append(tools, n) }}); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0] != "probe" || ft.callCount() != 1 {
		t.Fatalf("tools=%v calls=%d", tools, ft.callCount())
	}
}

func TestComposeDoesNotStore(t *testing.T) {
	e := newEnv(t, llmtest.Script{llmtest.Text("Hey, Atlas here.")})
	b := e.bot("Atlas")
	got, err := e.m.Compose(context.Background(), b.ID, "Write the opener.")
	if err != nil || got != "Hey, Atlas here." || len(e.history(b.ID)) != 0 {
		t.Fatalf("got=%q err=%v history=%d", got, err, len(e.history(b.ID)))
	}
}

func TestCallCardPostAndUpdate(t *testing.T) {
	e := newEnv(t, nil)
	b := e.bot("Atlas")
	id, err := e.m.PostCallCard(context.Background(), b.ID, MsgPart{Type: PartCall, ID: "c1", Name: "outbound", Status: "dialing", Title: "deploy failed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.UpdateCallCard(context.Background(), id, MsgPart{Type: PartCall, ID: "c1", Name: "outbound", Status: "completed", DurationMs: 61_000}); err != nil {
		t.Fatal(err)
	}
	h := e.history(b.ID)
	if len(h) != 1 || h[0].Parts[0].Status != "completed" || h[0].Parts[0].DurationMs != 61_000 {
		t.Fatalf("history %+v", h)
	}
}

func TestJarvisConfigValidationAndSaveCheck(t *testing.T) {
	e := newEnv(t, nil)
	b := e.bot("Atlas")
	in := BotInput{ID: b.ID, Name: "Atlas", Config: BotConfig{Jarvis: BotJarvis{Enabled: true, Policy: strings.Repeat("x", MaxJarvisPolicy+1)}}}
	if _, err := e.m.SaveBot(context.Background(), in); err == nil {
		t.Error("accepted an over-long policy")
	}
	e.m.SetSaveCheck(func(_ context.Context, in BotInput) error {
		if in.Config.Jarvis.Enabled {
			return invalidf("Jarvis needs this bot's tool access set to All.")
		}
		return nil
	})
	in.Config.Jarvis.Policy = "only failures"
	if _, err := e.m.SaveBot(context.Background(), in); err == nil || !strings.Contains(err.Error(), "tool access") {
		t.Errorf("save check not applied: %v", err)
	}
	in.Config.Jarvis.Enabled = false
	if _, err := e.m.SaveBot(context.Background(), in); err != nil {
		t.Errorf("disabled jarvis rejected: %v", err)
	}
}
```

Before running: check `internal/llm/llmtest` for the exact script helpers (`Text`, `Call`, `Block`) and the fake's last-request accessor; use the names that exist and add a `Block()` step (a stream that waits for ctx cancel) and `LastRequest()` to `llmtest` if missing, with their own small test.

- [ ] **Step 3: Run them and see them fail**

Run: `go test ./internal/chat/ -run 'TestVoiceTurn|TestCompose|TestCallCard|TestJarvisConfig'`
Expected: FAIL, undefined `VoiceTurn`.

- [ ] **Step 4: Engine hooks**

In `engine.go`:
- `turnEnv` gains `onText func(string)` and `onTool func(string)`.
- `prep` gains `voice bool`, `onText func(string)`, `onTool func(string)`.
- In `prepare`, after the existing reasoning block (engine.go:262-264):

```go
	if p.voice {
		env.reasoning = voiceReasoning(mdl.Reasoning)
		env.bot.Config.Limits = BotLimits{MaxSteps: voiceMaxSteps, MaxToolCalls: voiceMaxToolCalls}
		env.onText, env.onTool = p.onText, p.onTool
	}
```

- Pass `Voice: p.voice` into `systemInput`.
- In `consume`, in the `llm.DeltaText` case after `env.sink.appendText(PartText, d.Text)`:

```go
			if env.onText != nil {
				env.onText(d.Text)
			}
```

- At the top of `runTool`:

```go
	if env.onTool != nil {
		env.onTool(c.Name)
	}
```

In `voice.go`:

```go
const (
	voiceMaxSteps     = 6
	voiceMaxToolCalls = 10
	callPrefix        = "call:"
)

// voiceReasoning picks the cheapest reasoning level the model offers; "" when it has none to choose.
func voiceReasoning(levels []string) string {
	for _, l := range []string{"none", "minimal", "low"} {
		if slices.Contains(levels, l) {
			return l
		}
	}
	return ""
}
```

In `prompt.go` add `Voice bool` to `systemInput` and replace `b.WriteString(chatStyle)` with:

```go
	if in.Voice {
		b.WriteString(voiceStyle)
	} else {
		b.WriteString(chatStyle)
	}
```

and the constant:

```go
// voiceStyle replaces chatStyle on phone calls: replies are spoken by a text-to-speech voice.
const voiceStyle = `## How you talk
You are on a phone call with the owner. Everything you write is spoken aloud.
- Answer in one to three short sentences. Lead with the answer.
- No markdown, lists, code, links or emoji. Spell out identifiers so they are easy to say.
- Never name the app you run in or say where you are calling from. You may say your own name.
- When you start long work (a code task, research), say that it has started and keep talking; do not wait for it.
- If the owner says to leave it, acknowledge briefly and stop.`
```

- [ ] **Step 5: Implement `VoiceTurn`, `Compose`, call cards**

Append to `voice.go`:

```go
type VoiceLine struct{ Role, Text string }

type VoiceRequest struct {
	BotID, CallID, Text string
	Brief               string      // call context; untrusted parts already fenced by the caller
	History             []VoiceLine // earlier turns of this call, oldest first
	OnText              func(string)
	OnTool              func(name string)
}

// VoiceTurn answers one spoken utterance. It runs under its own run key, so a phone call never waits behind
// the bot's chat queue, and it stores the utterance and reply in the bot's thread like a typed exchange.
func (m *Manager) VoiceTurn(ctx context.Context, req VoiceRequest) (string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", invalidf("Nothing was said.")
	}
	bot, err := m.Bot(ctx, req.BotID)
	if err != nil {
		return "", err
	}
	key := callPrefix + req.CallID
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrClosed
	}
	if m.runs[key] != nil {
		m.mu.Unlock()
		return "", busyf("Still answering the previous sentence.")
	}
	st := &runState{cancel: cancel, done: make(chan struct{})}
	m.runs[key] = st
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.runs, key)
		close(st.done)
		m.mu.Unlock()
		m.wg.Done()
	}()

	bg := context.WithoutCancel(ctx)
	now := m.nowMs()
	user := Message{ID: newID(), BotID: bot.ID, Role: "user", Author: "user", Status: StatusComplete, CreatedAt: now, FinishedAt: now,
		Parts: []MsgPart{{Type: PartText, Text: req.Text}}}
	if err := m.store.insertMessage(bg, &user); err != nil {
		return "", err
	}
	m.publishMessage(user)
	asst := Message{ID: newID(), BotID: bot.ID, Role: "assistant", Author: "bot:" + bot.ID, Status: StatusRunning, CreatedAt: m.nowMs(), Parts: []MsgPart{}}
	if err := m.store.insertMessage(bg, &asst); err != nil {
		return "", err
	}
	persist := func(msg Message) error {
		pc, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		return m.store.updateMessage(pc, msg)
	}
	s := newSink(asst, persist, m.publishMessage, m.publishDelta)
	s.clock = m.cfg.Clock
	rc := RunContext{BotID: bot.ID, Bot: bot, MessageID: asst.ID, Actor: "bot:" + bot.ID, Author: "user", CallID: req.CallID, Emit: s.upsertTool}
	hist := make([]Message, 0, len(req.History))
	for _, l := range req.History {
		hist = append(hist, Message{Role: l.Role, Status: StatusComplete, Parts: []MsgPart{{Type: PartText, Text: l.Text}}})
	}
	env, err := m.prepare(runCtx, prep{bot: bot, convo: bot.ID, rc: rc, sink: s, text: req.Text, history: historyMessages(hist),
		memory: true, voice: true, extra: req.Brief, onText: req.OnText, onTool: req.OnTool})
	if err != nil {
		_ = s.finish(StatusError, describeError(err))
		return "", err
	}
	status, errText := m.loop(runCtx, env)
	_ = s.finish(status, errText)
	if status == StatusError {
		return s.finalText(), errors.New(errText)
	}
	return s.finalText(), nil
}

// Compose asks the bot's model for one short spoken line (the call opener). Nothing is stored and no tools run.
func (m *Manager) Compose(ctx context.Context, botID, instruction string) (string, error) {
	bot, err := m.Bot(ctx, botID)
	if err != nil {
		return "", err
	}
	asst := Message{ID: newID(), BotID: callPrefix + "compose", Role: "assistant", Parts: []MsgPart{}}
	s := newSink(asst, nil, func(Message) {}, func(MessageDelta) {})
	env, err := m.prepare(ctx, prep{bot: bot, rc: RunContext{BotID: bot.ID, Bot: bot, Actor: "bot:" + bot.ID}, sink: s, text: instruction, draft: true, voice: true})
	if err != nil {
		return "", err
	}
	ch, err := env.provider.Stream(ctx, llm.Request{Model: env.modelID, System: env.system,
		Messages: []llm.Message{{Role: "user", Parts: []llm.Part{{Type: llm.PartText, Text: instruction}}}}, Reasoning: env.reasoning, MaxOutputTokens: 200})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for d := range ch {
		switch d.Kind {
		case llm.DeltaText:
			b.WriteString(d.Text)
		case llm.DeltaError:
			drain(ch)
			return "", d.Err
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (m *Manager) PostCallCard(ctx context.Context, botID string, part MsgPart) (string, error) {
	part.Type = PartCall
	now := m.nowMs()
	msg := Message{ID: newID(), BotID: botID, Role: "assistant", Author: "bot:" + botID, Status: StatusComplete, CreatedAt: now, FinishedAt: now, Parts: []MsgPart{part}}
	if err := m.store.insertMessage(ctx, &msg); err != nil {
		return "", err
	}
	m.publishMessage(msg)
	return msg.ID, nil
}

func (m *Manager) UpdateCallCard(ctx context.Context, msgID string, part MsgPart) error {
	msg, err := m.store.getMessage(ctx, msgID)
	if err != nil {
		return err
	}
	part.Type = PartCall
	msg.Parts = []MsgPart{part}
	if err := m.store.updateMessage(ctx, msg); err != nil {
		return err
	}
	m.publishMessage(msg)
	return nil
}
```

Check the llm part type names (`llm.Part`, `llm.PartText`) in `internal/llm/types.go` and the existing `historyMessages` signature; adapt the two lines that use them to the real names. Imports for `voice.go`: `context`, `errors`, `slices`, `strings`, `time`, `internal/llm`.

- [ ] **Step 6: Validation and save check**

In `store.go` `validateConfig`, before `return normalizeConfig(c), nil`:

```go
	if len(c.Jarvis.Policy) > MaxJarvisPolicy || !utf8.ValidString(c.Jarvis.Policy) {
		return c, invalidf("The \"When to call me\" text is limited to %d characters.", MaxJarvisPolicy)
	}
	for _, v := range []string{c.Jarvis.VoiceID, c.Jarvis.TTSModel} {
		if len(v) > 128 || strings.ContainsAny(v, " \t\r\n") {
			return c, invalidf("The Jarvis voice or voice model is not valid.")
		}
	}
```

In `manager.go`: add a field `saveCheck func(context.Context, BotInput) error` guarded by `m.mu`, the setter

```go
// SetSaveCheck installs a check the daemon runs before a bot is saved (Jarvis needs full permissions).
func (m *Manager) SetSaveCheck(f func(ctx context.Context, in BotInput) error) {
	m.mu.Lock()
	m.saveCheck = f
	m.mu.Unlock()
}
```

and at the top of `SaveBot`:

```go
	m.mu.Lock()
	check := m.saveCheck
	m.mu.Unlock()
	if check != nil && in.Config.Jarvis.Enabled {
		if err := check(ctx, in); err != nil {
			return Bot{}, err
		}
	}
```

In `ImportBot` next to `in.Config.Executive = false` add `in.Config.Jarvis = BotJarvis{}`, and extend the existing import test to assert it.

- [ ] **Step 7: Run the tests until they pass**

Run: `go test ./internal/chat/... -race`
Expected: PASS (new and existing tests).

- [ ] **Step 8: Commit**

```bash
git add internal/chat/ internal/llm/llmtest/
git commit -m "feat(chat): add voice turns, call cards and the jarvis bot setting"
```

---

### Task 8: Bot tools for rules and calls

**Files:**
- Create: `internal/chat/tools/jarvis.go`, `internal/chat/tools/jarvis_test.go`

**Interfaces:**
- Consumes: `chat.Tool`, `chat.RunContext.CallID`, `BotConfig.Jarvis.Enabled`, helpers `decode`, `text`, `errResult`, `oneLine` from the tools package.
- Produces:
  - `type JarvisRuntime interface { AddRule(ctx, botID string, r JarvisRule) (JarvisRule, error); Rules(ctx, botID string) ([]JarvisRule, error); DeleteRule(ctx, botID, id string) error; CallOwner(ctx, botID, reason string, urgent bool) (string, error); SwitchBot(ctx, callID, name string) (string, error) }`
  - `type JarvisRule struct{ID, Trigger, Target, Note string; Repeat, Urgent, QuietOverride bool; ExpiresAt int64}`
  - `type Jarvis struct{ Runtime JarvisRuntime }` (tool ID `"jarvis"`), tool names `jarvis_rule_add`, `jarvis_rule_list`, `jarvis_rule_delete`, `call_owner`, `switch_bot`
  - `func JarvisPrompt(bot chat.Bot) string` (prompt section: policy text + how to use rules)

- [ ] **Step 1: Write the failing test**

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type fakeJarvis struct {
	added    []JarvisRule
	called   []string
	switched []string
}

func (f *fakeJarvis) AddRule(_ context.Context, botID string, r JarvisRule) (JarvisRule, error) {
	r.ID = "r1"
	f.added = append(f.added, r)
	return r, nil
}
func (f *fakeJarvis) Rules(context.Context, string) ([]JarvisRule, error) { return f.added, nil }
func (f *fakeJarvis) DeleteRule(context.Context, string, string) error   { return nil }
func (f *fakeJarvis) CallOwner(_ context.Context, botID, reason string, urgent bool) (string, error) {
	f.called = append(f.called, reason)
	return "Calling now.", nil
}
func (f *fakeJarvis) SwitchBot(_ context.Context, callID, name string) (string, error) {
	f.switched = append(f.switched, name)
	return "Switched to Nova.", nil
}

func names(specs []llm.ToolSpec) []string {
	var out []string
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

func TestJarvisSpecsGating(t *testing.T) {
	j := Jarvis{Runtime: &fakeJarvis{}}
	off := chat.Bot{ID: "b1"}
	if specs := j.Specs(off, llm.Caps{Tools: true}); specs != nil {
		t.Fatalf("offered to a bot without Jarvis: %v", names(specs))
	}
	on := chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}
	got := strings.Join(names(j.Specs(on, llm.Caps{Tools: true})), ",")
	if got != "jarvis_rule_add,jarvis_rule_list,jarvis_rule_delete,call_owner,switch_bot" {
		t.Fatalf("specs %s", got)
	}
}

func TestJarvisRuleAddParsesArgs(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	rc := chat.RunContext{BotID: "b1", Bot: chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}}
	args, _ := json.Marshal(map[string]any{"trigger": "task_finished", "target": "t1", "note": "deploy", "repeat": false})
	res, err := j.Call(context.Background(), rc, "jarvis_rule_add", args)
	if err != nil || res.IsError || len(f.added) != 1 || f.added[0].Target != "t1" {
		t.Fatalf("res=%+v err=%v added=%+v", res, err, f.added)
	}
}

func TestSwitchBotOnlyOnCalls(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	rc := chat.RunContext{BotID: "b1", Bot: chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}}
	args := json.RawMessage(`{"name":"Nova"}`)
	res, _ := j.Call(context.Background(), rc, "switch_bot", args)
	if !res.IsError || len(f.switched) != 0 {
		t.Fatal("switch_bot worked outside a call")
	}
	rc.CallID = "c1"
	res, _ = j.Call(context.Background(), rc, "switch_bot", args)
	if res.IsError || len(f.switched) != 1 {
		t.Fatalf("switch_bot on a call failed: %+v", res)
	}
}

func TestCallOwnerRefusedDuringCall(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	rc := chat.RunContext{BotID: "b1", CallID: "c1", Bot: chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}}
	res, _ := j.Call(context.Background(), rc, "call_owner", json.RawMessage(`{"reason":"x"}`))
	if !res.IsError || len(f.called) != 0 {
		t.Fatal("call_owner placed a call while already on one")
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/chat/tools/ -run 'TestJarvis|TestSwitchBot|TestCallOwner'`
Expected: FAIL, undefined `Jarvis`.

- [ ] **Step 3: Implement `jarvis.go`**

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type JarvisRule struct {
	ID            string `json:"id,omitempty"`
	Trigger       string `json:"trigger"`
	Target        string `json:"target"`
	Note          string `json:"note,omitempty"`
	Repeat        bool   `json:"repeat,omitempty"`
	Urgent        bool   `json:"urgent,omitempty"`
	QuietOverride bool   `json:"quietOverride,omitempty"`
	ExpiresAt     int64  `json:"expiresAt,omitempty"` // unix ms
}

type JarvisRuntime interface {
	AddRule(ctx context.Context, botID string, r JarvisRule) (JarvisRule, error)
	Rules(ctx context.Context, botID string) ([]JarvisRule, error)
	DeleteRule(ctx context.Context, botID, id string) error
	CallOwner(ctx context.Context, botID, reason string, urgent bool) (string, error)
	SwitchBot(ctx context.Context, callID, name string) (string, error)
}

// Jarvis offers phone tools to bots with Jarvis mode on. The daemon re-checks permissions on every call.
type Jarvis struct{ Runtime JarvisRuntime }

func (Jarvis) ID() string { return "jarvis" }

const ruleSchema = `{"type":"object","properties":{
"trigger":{"type":"string","enum":["task_finished","task_failed","automation_finished","automation_failed","notification","at_time"]},
"target":{"type":"string","description":"Task or automation id, \"any\" (for tasks: any task you started), a notification kind like \"failed\" or \"failed:github\", or HH:MM for at_time."},
"note":{"type":"string","maxLength":280,"description":"What to tell the owner when it fires."},
"repeat":{"type":"boolean","description":"Keep the rule after it fires. Default: delete after the first call."},
"urgent":{"type":"boolean","description":"Retry once after 5 minutes if unanswered."},
"quietOverride":{"type":"boolean","description":"Call even during quiet hours. Only when the owner says so."},
"expiresAt":{"type":"integer","description":"Unix milliseconds after which the rule no longer fires (e.g. \"tonight\")."}},
"required":["trigger","target"]}`

func (j Jarvis) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools || !bot.Config.Jarvis.Enabled || j.Runtime == nil {
		return nil
	}
	return []llm.ToolSpec{
		{Name: "jarvis_rule_add", Description: "Save a rule for when to phone the owner, after they asked for it (\"call me when the deploy finishes\"). Confirm the saved rule in your reply.", Schema: json.RawMessage(ruleSchema)},
		{Name: "jarvis_rule_list", Description: "List your call rules.", Schema: json.RawMessage(`{"type":"object","properties":{}}`)},
		{Name: "jarvis_rule_delete", Description: "Delete one of your call rules.", Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)},
		{Name: "call_owner", Description: "Phone the owner now because something matches their \"When to call me\" policy. The app may refuse (quiet hours, limits).", Schema: json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","maxLength":200},"urgent":{"type":"boolean"}},"required":["reason"]}`)},
		{Name: "switch_bot", Description: "On a phone call only: hand the call to another bot by name when the owner asks for it.", Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)},
	}
}

func (j Jarvis) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if !rc.Bot.Config.Jarvis.Enabled {
		return errResult("Jarvis mode is off for this bot.")
	}
	switch name {
	case "jarvis_rule_add":
		var in JarvisRule
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		r, err := j.Runtime.AddRule(ctx, rc.BotID, in)
		if err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: fmt.Sprintf("Saved rule %s: %s %s%s.", r.ID, r.Trigger, r.Target, map[bool]string{true: " (repeats)", false: " (once)"}[r.Repeat])}, nil
	case "jarvis_rule_list":
		rules, err := j.Runtime.Rules(ctx, rc.BotID)
		if err != nil {
			return errResult("%s", err)
		}
		if len(rules) == 0 {
			return chat.ToolResult{Text: "No call rules."}, nil
		}
		var b strings.Builder
		for _, r := range rules {
			fmt.Fprintf(&b, "- %s: %s %s once=%v note=%q\n", r.ID, r.Trigger, r.Target, !r.Repeat, oneLine(r.Note, 120))
		}
		return chat.ToolResult{Text: b.String()}, nil
	case "jarvis_rule_delete":
		var in struct{ ID string `json:"id"` }
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		if err := j.Runtime.DeleteRule(ctx, rc.BotID, in.ID); err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: "Deleted."}, nil
	case "call_owner":
		if rc.CallID != "" {
			return errResult("You are already on a call with the owner.")
		}
		var in struct {
			Reason string `json:"reason"`
			Urgent bool   `json:"urgent"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		msg, err := j.Runtime.CallOwner(ctx, rc.BotID, oneLine(in.Reason, 200), in.Urgent)
		if err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: msg}, nil
	case "switch_bot":
		if rc.CallID == "" {
			return errResult("switch_bot only works on a phone call.")
		}
		var in struct{ Name string `json:"name"` }
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		msg, err := j.Runtime.SwitchBot(ctx, rc.CallID, in.Name)
		if err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: msg}, nil
	}
	return errResult("Unknown tool %s.", name)
}

// JarvisPrompt adds the owner's call policy to Jarvis-enabled bots.
func JarvisPrompt(bot chat.Bot) string {
	if !bot.Config.Jarvis.Enabled {
		return ""
	}
	policy := strings.TrimSpace(bot.Config.Jarvis.Policy)
	if policy == "" {
		policy = "Only call for failures or when you need a decision."
	}
	return "## Phone calls (Jarvis)\nYou can phone the owner. When they tell you when to call, save it with jarvis_rule_add and confirm it. " +
		"Use call_owner only when something matches their policy below; the app enforces quiet hours and limits.\n" +
		"Owner's policy, in their words:\n" + chat.Fence("owner policy", policy)
}

var _ chat.Tool = Jarvis{}
```

Check that `promptSections` skips empty strings; if it does not, return `""` handling there by filtering empties in `Manager.promptSections`.

- [ ] **Step 4: Run the tests until they pass**

Run: `go test ./internal/chat/tools/ -race -v -run 'TestJarvis|TestSwitchBot|TestCallOwner'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/chat/tools/jarvis.go internal/chat/tools/jarvis_test.go
git commit -m "feat(chat): add jarvis rule and call tools for bots"
```

---

### Task 9: Call manager

**Files:**
- Create: `internal/jarvis/calls.go`, `internal/jarvis/calls_test.go`

**Interfaces:**
- Consumes: `Store`, `Settings`, `CheckOpener`, `FallbackOpener`, `Frame*`, `ElevenLabs.OutboundCall`, `ElevenLabs.ConversationStatus`, `chat.VoiceRequest`, `chat.VoiceLine`, `chat.MsgPart`, `chat.PartCall`.
- Produces:
  - `type Host interface` (implemented by the daemon in Task 10):
    ```go
    type Host interface {
        Bot(ctx context.Context, id string) (chat.Bot, error)
        Bots(ctx context.Context) ([]chat.Bot, error)
        Gate(ctx context.Context, bot chat.Bot) error
        VoiceTurn(ctx context.Context, req chat.VoiceRequest) (string, error)
        Compose(ctx context.Context, botID, instruction string) (string, error)
        PostCallCard(ctx context.Context, botID string, part chat.MsgPart) (string, error)
        UpdateCallCard(ctx context.Context, msgID string, part chat.MsgPart) error
        Notify(title, body, target, level string)
        Audit(action, target, outcome, detail string)
        DisableJarvis(ctx context.Context, botID, reason string)
    }
    ```
  - `type Dialer interface{ OutboundCall(ctx, OutboundCall) (string, string, error); ConversationStatus(ctx, id string) (string, int, error) }`
  - `type Sender interface{ Send(ctx, Frame) error; Connected() bool }`
  - `type Calls struct` with `func NewCalls(store *Store, host Host, dial Dialer, relay Sender, settings func() Settings, now func() time.Time, log *slog.Logger) *Calls`
  - `(*Calls) Place(ctx, PlaceRequest) (Call, error)`; `type PlaceRequest struct{BotID, Reason, Brief, RuleID string; Urgent bool; Verify string}` (`Verify` = code for a verification call)
  - `(*Calls) Active() (Call, bool)`; `(*Calls) Inject(text string)` (adds to the live call's brief)
  - `(*Calls) HandleFrame(ctx, Frame)` (llm.request, inbound.lookup, call.status)
  - `(*Calls) SwitchBot(ctx, callID, name string) (string, error)`

- [ ] **Step 1: Write the failing tests** (fake host, dialer and relay)

```go
package jarvis

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
)

type fakeHost struct {
	mu        sync.Mutex
	bots      map[string]chat.Bot
	gateErr   error
	opener    string
	turns     []chat.VoiceRequest
	reply     string
	cards     map[string]chat.MsgPart
	notices   []string
	disabled  []string
	toolDuringTurn string
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		bots: map[string]chat.Bot{
			"b1": {ID: "b1", Name: "Atlas", Config: chat.BotConfig{Executive: true, Jarvis: chat.BotJarvis{Enabled: true}}},
			"b2": {ID: "b2", Name: "Nova", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}},
			"b3": {ID: "b3", Name: "Quiet"},
		},
		opener: "Hey, Atlas here. The deploy failed.",
		reply:  "Two tasks are running.",
		cards:  map[string]chat.MsgPart{},
	}
}

func (h *fakeHost) Bot(_ context.Context, id string) (chat.Bot, error) {
	h.mu.Lock(); defer h.mu.Unlock()
	b, ok := h.bots[id]
	if !ok { return b, errors.New("no bot") }
	return b, nil
}
func (h *fakeHost) Bots(context.Context) ([]chat.Bot, error) {
	h.mu.Lock(); defer h.mu.Unlock()
	return []chat.Bot{h.bots["b1"], h.bots["b2"], h.bots["b3"]}, nil
}
func (h *fakeHost) Gate(_ context.Context, b chat.Bot) error {
	h.mu.Lock(); defer h.mu.Unlock()
	if !b.Config.Jarvis.Enabled { return &GateError{"off"} }
	return h.gateErr
}
func (h *fakeHost) VoiceTurn(_ context.Context, r chat.VoiceRequest) (string, error) {
	h.mu.Lock(); h.turns = append(h.turns, r); tool := h.toolDuringTurn; h.mu.Unlock()
	if tool != "" && r.OnTool != nil { r.OnTool(tool) }
	r.OnText(h.reply)
	return h.reply, nil
}
func (h *fakeHost) Compose(context.Context, string, string) (string, error) { return h.opener, nil }
func (h *fakeHost) PostCallCard(_ context.Context, _ string, p chat.MsgPart) (string, error) {
	h.mu.Lock(); defer h.mu.Unlock(); h.cards["m-"+p.ID] = p; return "m-" + p.ID, nil
}
func (h *fakeHost) UpdateCallCard(_ context.Context, id string, p chat.MsgPart) error {
	h.mu.Lock(); defer h.mu.Unlock(); h.cards[id] = p; return nil
}
func (h *fakeHost) Notify(title, body, target, level string) { h.mu.Lock(); h.notices = append(h.notices, title+": "+body); h.mu.Unlock() }
func (h *fakeHost) Audit(string, string, string, string)     {}
func (h *fakeHost) DisableJarvis(_ context.Context, id, _ string) { h.mu.Lock(); h.disabled = append(h.disabled, id); h.mu.Unlock() }

type fakeDialer struct{ calls []OutboundCall; status string; err error }

func (d *fakeDialer) OutboundCall(_ context.Context, c OutboundCall) (string, string, error) {
	d.calls = append(d.calls, c)
	return "conv-" + c.CallID, "CA1", d.err
}
func (d *fakeDialer) ConversationStatus(context.Context, string) (string, int, error) { return d.status, 0, nil }

type fakeSender struct{ mu sync.Mutex; frames []Frame; down bool }

func (s *fakeSender) Send(_ context.Context, f Frame) error {
	if s.down { return ErrRelayDown }
	s.mu.Lock(); s.frames = append(s.frames, f); s.mu.Unlock(); return nil
}
func (s *fakeSender) Connected() bool { return !s.down }
func (s *fakeSender) text(id string) string {
	s.mu.Lock(); defer s.mu.Unlock()
	var b strings.Builder
	for _, f := range s.frames { if f.ID == id && f.Type == FrameLLMDelta { b.WriteString(f.Text) } }
	return b.String()
}

type callEnv struct {
	c     *Calls
	host  *fakeHost
	dial  *fakeDialer
	relay *fakeSender
	store *Store
	now   *time.Time
}

func newCallEnv(t *testing.T) *callEnv {
	st, now := testStore(t)
	*now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e := &callEnv{host: newFakeHost(), dial: &fakeDialer{}, relay: &fakeSender{}, store: st, now: now}
	settings := Settings{OwnerNumber: "+32470000000", OwnerVerified: true, AgentID: "ag", PhoneNumberID: "pn"}
	e.c = NewCalls(st, e.host, e.dial, e.relay, func() Settings { return settings }, func() time.Time { return *e.now }, slog.Default())
	return e
}

func llmFrame(id, callID, text string) Frame {
	return Frame{Type: FrameLLMRequest, ID: id, CallID: callID, Messages: []LLMMessage{{Role: "user", Content: text}}}
}

func TestOutboundCallSpeaksCachedOpenerFirst(t *testing.T) {
	e := newCallEnv(t)
	call, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "the deploy failed"})
	if err != nil { t.Fatal(err) }
	if len(e.dial.calls) != 1 || e.dial.calls[0].To != "+32470000000" || e.dial.calls[0].CallID != call.ID {
		t.Fatalf("dialed %+v", e.dial.calls)
	}
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID}) // first turn: no user message yet
	if got := e.relay.text("r0"); got != e.host.opener {
		t.Fatalf("first turn spoke %q", got)
	}
	if len(e.host.turns) != 0 {
		t.Fatal("opener ran a model turn")
	}
}

func TestOpenerFallbackWhenInvalid(t *testing.T) {
	e := newCallEnv(t)
	e.host.opener = "Hi from Orole!"
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "the deploy failed"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID})
	got := e.relay.text("r0")
	if !strings.Contains(got, "Atlas") || strings.Contains(strings.ToLower(got), "orole") {
		t.Fatalf("fallback %q", got)
	}
}

func TestSpokenTurnRunsVoiceTurnWithFiller(t *testing.T) {
	e := newCallEnv(t)
	e.host.toolDuringTurn = "list_tasks"
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "what's running?"))
	got := e.relay.text("r1")
	if !strings.HasPrefix(got, "Checking your tasks.") || !strings.HasSuffix(got, "Two tasks are running.") {
		t.Fatalf("spoken %q", got)
	}
	if e.host.turns[0].Text != "what's running?" || e.host.turns[0].CallID != call.ID {
		t.Fatalf("turn %+v", e.host.turns[0])
	}
}

func TestGateFailureMidCallDisablesJarvis(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.host.gateErr = &GateError{"Jarvis needs this bot's tool access set to All."}
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "start a task"))
	if len(e.host.turns) != 0 {
		t.Fatal("turn ran after the gate failed")
	}
	if !strings.Contains(e.relay.text("r1"), "tool access") || len(e.host.disabled) != 1 {
		t.Fatalf("spoken=%q disabled=%v", e.relay.text("r1"), e.host.disabled)
	}
}

func TestSecondRuleDuringCallIsInjected(t *testing.T) {
	e := newCallEnv(t)
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b2", Reason: "the deploy finished"}); err != ErrInjected {
		t.Fatalf("second place: %v", err)
	}
	if len(e.dial.calls) != 1 {
		t.Fatal("dialed twice")
	}
	call, _ := e.c.Active()
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "anything else?"))
	if !strings.Contains(e.host.turns[0].Brief, "the deploy finished") {
		t.Fatalf("injection not in brief: %q", e.host.turns[0].Brief)
	}
}

func TestUnknownCallGetsGoodbye(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), llmFrame("r1", "gone", "hello?"))
	got := e.relay.text("r1")
	if got == "" || len(e.host.turns) != 0 {
		t.Fatalf("spoken %q turns %d", got, len(e.host.turns))
	}
	var hung bool
	for _, f := range e.relay.frames { hung = hung || (f.Type == FrameHangup && f.CallID == "gone") }
	if !hung { t.Fatal("no hangup for unknown call") }
}

func TestInboundOnlyFromOwner(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i1", Caller: "+15550000000"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i2", Caller: "+32470000000"})
	var a1, a2 Frame
	for _, f := range e.relay.frames {
		if f.ID == "i1" { a1 = f }
		if f.ID == "i2" { a2 = f }
	}
	if a1.Accept || !a2.Accept || a2.Vars["call_id"] == "" || a2.Vars["bot_id"] != "b1" {
		t.Fatalf("a1=%+v a2=%+v", a1, a2)
	}
	if len(e.host.notices) != 1 {
		t.Fatalf("inbound call not notified: %v", e.host.notices)
	}
}

func TestSwitchBotOnCall(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i1", Caller: "+32470000000"})
	call, _ := e.c.Active()
	if _, err := e.c.SwitchBot(context.Background(), call.ID, "quiet"); err == nil {
		t.Fatal("switched to a bot without Jarvis")
	}
	if _, err := e.c.SwitchBot(context.Background(), call.ID, "nova"); err != nil {
		t.Fatal(err)
	}
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi"))
	if e.host.turns[0].BotID != "b2" {
		t.Fatalf("turn went to %s", e.host.turns[0].BotID)
	}
}

func TestVoicemailBecomesNotice(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "the deploy failed"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: call.ID, Status: "voicemail"})
	got, _ := e.store.GetCall(context.Background(), call.ID)
	if got.Status != StatusVoicemail || len(e.host.notices) != 1 || !strings.Contains(e.host.notices[0], "Atlas tried to call") {
		t.Fatalf("call=%+v notices=%v", got, e.host.notices)
	}
	if _, active := e.c.Active(); active {
		t.Fatal("call still active")
	}
}

func TestRelayDownRefusesCall(t *testing.T) {
	e := newCallEnv(t)
	e.relay.down = true
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"}); err == nil {
		t.Fatal("placed a call with the relay down")
	}
}
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/jarvis/ -run 'TestOutbound|TestOpener|TestSpoken|TestGateFailure|TestSecondRule|TestUnknownCall|TestInbound|TestSwitchBot|TestVoicemail|TestRelayDown'`
Expected: FAIL, undefined `NewCalls`.

- [ ] **Step 3: Implement `calls.go`**

```go
package jarvis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/chat"
)

var ErrInjected = errors.New("jarvis: added to the call in progress")

type Host interface {
	Bot(ctx context.Context, id string) (chat.Bot, error)
	Bots(ctx context.Context) ([]chat.Bot, error)
	Gate(ctx context.Context, bot chat.Bot) error
	VoiceTurn(ctx context.Context, req chat.VoiceRequest) (string, error)
	Compose(ctx context.Context, botID, instruction string) (string, error)
	PostCallCard(ctx context.Context, botID string, part chat.MsgPart) (string, error)
	UpdateCallCard(ctx context.Context, msgID string, part chat.MsgPart) error
	Notify(title, body, target, level string)
	Audit(action, target, outcome, detail string)
	DisableJarvis(ctx context.Context, botID, reason string)
}

type Dialer interface {
	OutboundCall(ctx context.Context, c OutboundCall) (string, string, error)
	ConversationStatus(ctx context.Context, id string) (string, int, error)
}

type Sender interface {
	Send(ctx context.Context, f Frame) error
	Connected() bool
}

type PlaceRequest struct {
	BotID, Reason, Brief, RuleID string
	Urgent                       bool
	Verify                       string // digits for a phone verification call; no bot turns
}

// live is the in-memory state of the one call in progress.
type live struct {
	call    Call
	botName string
	brief   string
	inject  []string
	opener  chan string // receives the opener once; closed after
	history []chat.VoiceLine
	verify  string
	urgent  bool
	spoken  bool // opener delivered
}

type Calls struct {
	store    *Store
	host     Host
	dial     Dialer
	relay    Sender
	settings func() Settings
	now      func() time.Time
	log      *slog.Logger

	mu   sync.Mutex
	cur  *live
	seed int
}

func NewCalls(store *Store, host Host, dial Dialer, relay Sender, settings func() Settings, now func() time.Time, log *slog.Logger) *Calls {
	return &Calls{store: store, host: host, dial: dial, relay: relay, settings: settings, now: now, log: log}
}

func (c *Calls) Active() (Call, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil {
		return Call{}, false
	}
	return c.cur.call, true
}

// Inject adds a line of news to the live call; the bot sees it in its next turn's brief.
func (c *Calls) Inject(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur != nil {
		c.cur.inject = append(c.cur.inject, text)
	}
}

func (c *Calls) Place(ctx context.Context, req PlaceRequest) (Call, error) {
	s := c.settings()
	if s.AgentID == "" || s.PhoneNumberID == "" || s.OwnerNumber == "" {
		return Call{}, &Refusal{"Phone calls are not set up yet."}
	}
	if !c.relay.Connected() {
		return Call{}, &Refusal{"The relay is not connected."}
	}
	bot, err := c.host.Bot(ctx, req.BotID)
	if err != nil {
		return Call{}, err
	}
	if req.Verify == "" {
		if err := c.host.Gate(ctx, bot); err != nil {
			return Call{}, err
		}
	}
	c.mu.Lock()
	if c.cur != nil {
		c.cur.inject = append(c.cur.inject, bot.Name+": "+req.Reason)
		c.mu.Unlock()
		return Call{}, ErrInjected
	}
	call := Call{ID: uuid.NewString(), BotID: bot.ID, Direction: Outbound, RuleID: req.RuleID, Reason: req.Reason, Status: StatusDialing, StartedAt: c.now().UnixMilli()}
	l := &live{call: call, botName: bot.Name, brief: req.Brief, opener: make(chan string, 1), verify: req.Verify, urgent: req.Urgent}
	c.cur = l
	c.seed++
	seed := c.seed
	c.mu.Unlock()

	if err := c.store.InsertCall(ctx, &l.call); err != nil {
		c.clear(l)
		return Call{}, err
	}
	if req.Verify == "" {
		if id, err := c.host.PostCallCard(ctx, bot.ID, c.card(l.call)); err == nil {
			l.call.MessageID = id
		}
	}
	conv, sid, err := c.dial.OutboundCall(ctx, OutboundCall{AgentID: s.AgentID, PhoneNumberID: s.PhoneNumberID, To: s.OwnerNumber, CallID: call.ID,
		BotID: bot.ID, VoiceID: or2(bot.Config.Jarvis.VoiceID, s.DefaultVoiceID), TTSModel: or2(bot.Config.Jarvis.TTSModel, s.DefaultTTSModel)})
	if err != nil {
		c.finish(ctx, l, StatusFailed, 0)
		c.host.Notify(bot.Name+" could not call", err.Error(), "bot:"+bot.ID, "error")
		return Call{}, err
	}
	c.mu.Lock()
	l.call.ConversationID, l.call.ProviderCallID = conv, sid
	c.mu.Unlock()
	_ = c.store.UpdateCall(ctx, l.call)
	c.host.Audit("jarvis.call.started", call.ID, "allowed", bot.Name+": "+req.Reason)

	// Write the opener while the phone rings.
	go func() {
		if req.Verify != "" {
			l.opener <- "Your verification code is " + spaced(req.Verify) + ". Again, " + spaced(req.Verify) + "."
			return
		}
		octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		text, err := c.host.Compose(octx, bot.ID, openerInstruction(bot.Name, req.Reason, req.Brief))
		if ok := err == nil; ok {
			if t, valid := CheckOpener(bot.Name, text); valid {
				l.opener <- t
				return
			}
		}
		l.opener <- FallbackOpener(bot.Name, req.Reason, seed)
	}()
	return l.call, nil
}

func openerInstruction(name, reason, brief string) string {
	return fmt.Sprintf("You are phoning the owner right now. Write the very first sentence you will say when they pick up. "+
		"Start with your name (%s) in a natural way, then say why you are calling: %s. One or two short sentences, no greeting formula, "+
		"never name the app. Context:\n%s", name, reason, brief)
}

func spaced(digits string) string { return strings.Join(strings.Split(digits, ""), " ") }

func or2(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (c *Calls) card(call Call) chat.MsgPart {
	dur := int64(0)
	if call.AnsweredAt > 0 && call.EndedAt > 0 {
		dur = call.EndedAt - call.AnsweredAt
	}
	summary := ""
	if call.EstCostMicros > 0 {
		summary = fmt.Sprintf("est. $%.2f", float64(call.EstCostMicros)/1e6)
	}
	return chat.MsgPart{Type: chat.PartCall, ID: call.ID, Name: call.Direction, Status: call.Status, Title: call.Reason, DurationMs: dur, Summary: summary}
}

func (c *Calls) HandleFrame(ctx context.Context, f Frame) {
	switch f.Type {
	case FrameLLMRequest:
		c.turn(ctx, f)
	case FrameInboundLookup:
		c.inbound(ctx, f)
	case FrameCallStatus:
		c.status(ctx, f)
	}
}

func (c *Calls) say(ctx context.Context, id, callID, text string) {
	_ = c.relay.Send(ctx, Frame{Type: FrameLLMDelta, ID: id, CallID: callID, Text: text})
}

func (c *Calls) done(ctx context.Context, id, callID string) {
	_ = c.relay.Send(ctx, Frame{Type: FrameLLMDone, ID: id, CallID: callID})
}

func (c *Calls) turn(ctx context.Context, f Frame) {
	c.mu.Lock()
	l := c.cur
	if l == nil || l.call.ID != f.CallID {
		c.mu.Unlock()
		c.say(ctx, f.ID, f.CallID, "Sorry, this call has ended. I'll message you instead.")
		c.done(ctx, f.ID, f.CallID)
		_ = c.relay.Send(ctx, Frame{Type: FrameHangup, CallID: f.CallID})
		return
	}
	if l.call.AnsweredAt == 0 {
		l.call.AnsweredAt = c.now().UnixMilli()
		l.call.Status = StatusInProgress
	}
	first := !l.spoken
	l.spoken = true
	c.mu.Unlock()

	utter := lastUser(f.Messages)
	if first && utter == "" {
		var text string
		select {
		case text = <-l.opener:
		case <-time.After(3 * time.Second):
			text = FallbackOpener(l.botName, l.call.Reason, 0)
		}
		if l.call.BotID == "" { // inbound before a bot is chosen
			text = text
		}
		c.mu.Lock()
		l.history = append(l.history, chat.VoiceLine{Role: "assistant", Text: text})
		c.mu.Unlock()
		c.say(ctx, f.ID, f.CallID, text)
		c.done(ctx, f.ID, f.CallID)
		if l.verify != "" {
			_ = c.relay.Send(ctx, Frame{Type: FrameHangup, CallID: f.CallID})
		}
		return
	}
	if l.verify != "" || utter == "" {
		c.done(ctx, f.ID, f.CallID)
		return
	}
	bot, err := c.host.Bot(ctx, l.call.BotID)
	if err == nil {
		err = c.host.Gate(ctx, bot)
	}
	if err != nil {
		var ge *GateError
		if errors.As(err, &ge) {
			c.host.DisableJarvis(ctx, l.call.BotID, ge.Reason)
		}
		c.say(ctx, f.ID, f.CallID, "I can't act on that: "+err.Error())
		c.done(ctx, f.ID, f.CallID)
		return
	}
	c.mu.Lock()
	brief := l.brief
	if len(l.inject) > 0 {
		brief += "\n\nNews since the call started:\n- " + strings.Join(l.inject, "\n- ")
	}
	history := append([]chat.VoiceLine(nil), l.history...)
	c.mu.Unlock()
	reply, err := c.host.VoiceTurn(ctx, chat.VoiceRequest{BotID: bot.ID, CallID: l.call.ID, Text: utter, Brief: brief, History: history,
		OnText: func(s string) { c.say(ctx, f.ID, f.CallID, s) },
		OnTool: func(name string) { c.say(ctx, f.ID, f.CallID, filler(name)+" ") },
	})
	if err != nil {
		c.say(ctx, f.ID, f.CallID, " That failed: "+firstSentence(err.Error()))
	}
	c.mu.Lock()
	l.history = append(l.history, chat.VoiceLine{Role: "user", Text: utter}, chat.VoiceLine{Role: "assistant", Text: reply})
	c.mu.Unlock()
	c.done(ctx, f.ID, f.CallID)
}

func lastUser(ms []LLMMessage) string {
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Role == "user" {
			return strings.TrimSpace(ms[i].Content)
		}
		if ms[i].Role == "assistant" {
			return ""
		}
	}
	return ""
}

func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return s[:i+1]
	}
	return s
}

var fillers = map[string]string{
	"list_tasks": "Checking your tasks.", "task_status": "Looking at that task.", "start_task": "Starting that coding task now.",
	"message_task": "Passing that on.", "stop_task": "Stopping it.", "list_automations": "Checking your automations.",
	"automation_runs": "Checking the runs.", "research": "Starting the research.", "web_search": "Looking that up.",
	"web_fetch": "Reading that.", "switch_bot": "One moment.",
}

func filler(tool string) string {
	if f, ok := fillers[tool]; ok {
		return f
	}
	return "One moment."
}

func (c *Calls) inbound(ctx context.Context, f Frame) {
	s := c.settings()
	reject := func(why string) {
		_ = c.relay.Send(ctx, Frame{Type: FrameInboundAnswer, ID: f.ID, Accept: false})
		c.host.Audit("jarvis.inbound.rejected", f.Caller, "denied", why)
	}
	if !s.OwnerVerified || f.Caller == "" || f.Caller != s.OwnerNumber {
		reject("caller is not the verified owner number")
		return
	}
	c.mu.Lock()
	busy := c.cur != nil
	c.mu.Unlock()
	if busy {
		reject("a call is already in progress")
		return
	}
	bot, ok := c.pickInboundBot(ctx)
	if !ok {
		reject("no bot has Jarvis mode on")
		return
	}
	call := Call{ID: uuid.NewString(), BotID: bot.ID, Direction: Inbound, Reason: "You called", Status: StatusInProgress, StartedAt: c.now().UnixMilli(), AnsweredAt: c.now().UnixMilli(), ProviderCallID: f.CallSID}
	l := &live{call: call, botName: bot.Name, opener: make(chan string, 1)}
	c.mu.Lock()
	c.cur = l
	c.mu.Unlock()
	_ = c.store.InsertCall(ctx, &l.call)
	if id, err := c.host.PostCallCard(ctx, bot.ID, c.card(l.call)); err == nil {
		l.call.MessageID = id
	}
	go func() {
		octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		text, err := c.host.Compose(octx, bot.ID, fmt.Sprintf("The owner just phoned you. Write your greeting: say your name (%s) naturally and ask what they need. One short sentence, never name the app.", bot.Name))
		if t, ok := CheckOpener(bot.Name, text); err == nil && ok {
			l.opener <- t
			return
		}
		l.opener <- "Hey, " + bot.Name + " here. What do you need?"
	}()
	vars := map[string]string{"call_id": call.ID, "bot_id": bot.ID}
	tts := map[string]string{}
	if v := or2(bot.Config.Jarvis.VoiceID, s.DefaultVoiceID); v != "" {
		tts["voice_id"] = v
	}
	if m := or2(bot.Config.Jarvis.TTSModel, s.DefaultTTSModel); m != "" {
		tts["model_id"] = m
	}
	ov, _ := jsonRaw(map[string]any{"tts": tts})
	_ = c.relay.Send(ctx, Frame{Type: FrameInboundAnswer, ID: f.ID, Accept: true, Vars: vars, Overrides: ov})
	c.host.Notify("Incoming call to "+bot.Name, "From your phone number.", "bot:"+bot.ID, "info")
	c.host.Audit("jarvis.call.started", call.ID, "allowed", "inbound to "+bot.Name)
}

// pickInboundBot returns the first executive bot that passes the gate, else the first bot that does.
func (c *Calls) pickInboundBot(ctx context.Context) (chat.Bot, bool) {
	bots, err := c.host.Bots(ctx)
	if err != nil {
		return chat.Bot{}, false
	}
	var fallback *chat.Bot
	for i := range bots {
		b := bots[i]
		if c.host.Gate(ctx, b) != nil {
			continue
		}
		if b.Config.Executive {
			return b, true
		}
		if fallback == nil {
			fallback = &bots[i]
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return chat.Bot{}, false
}

func (c *Calls) SwitchBot(ctx context.Context, callID, name string) (string, error) {
	bots, err := c.host.Bots(ctx)
	if err != nil {
		return "", err
	}
	for _, b := range bots {
		if !strings.EqualFold(b.Name, strings.TrimSpace(name)) {
			continue
		}
		if err := c.host.Gate(ctx, b); err != nil {
			return "", fmt.Errorf("%s isn't set up for calls.", b.Name)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.cur == nil || c.cur.call.ID != callID {
			return "", errors.New("This call has ended.")
		}
		c.cur.call.BotID, c.cur.botName = b.ID, b.Name
		c.cur.history = nil
		return fmt.Sprintf("Handed over. %s answers from the next sentence and should introduce itself by name.", b.Name), nil
	}
	return "", fmt.Errorf("There is no bot called %s.", name)
}

func (c *Calls) status(ctx context.Context, f Frame) {
	c.mu.Lock()
	l := c.cur
	if l == nil || l.call.ID != f.CallID {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	st := StatusCompleted
	switch strings.ToLower(f.Status) {
	case "voicemail", "voicemail_detected":
		st = StatusVoicemail
	case "no-answer", "no_answer", "busy", "canceled":
		st = StatusNoAnswer
	case "failed":
		st = StatusFailed
	}
	if st == StatusCompleted && l.call.AnsweredAt == 0 {
		st = StatusNoAnswer
	}
	c.finish(ctx, l, st, f.DurationSecs)
}

func (c *Calls) finish(ctx context.Context, l *live, status string, durationSecs int) {
	c.mu.Lock()
	l.call.Status = status
	l.call.EndedAt = c.now().UnixMilli()
	if durationSecs > 0 {
		l.call.EstCostMicros = int64(durationSecs) * int64(c.settings().centsPerMinute()) * 10_000 / 60
	}
	call := l.call
	name := l.botName
	if c.cur == l {
		c.cur = nil
	}
	c.mu.Unlock()
	_ = c.store.UpdateCall(ctx, call)
	if call.MessageID != "" {
		_ = c.host.UpdateCallCard(ctx, call.MessageID, c.card(call))
	}
	c.host.Audit("jarvis.call.ended", call.ID, "allowed", status)
	if (status == StatusNoAnswer || status == StatusVoicemail) && l.verify == "" {
		c.host.Notify(name+" tried to call", call.Reason, "bot:"+call.BotID, "warning")
		if l.urgent {
			go c.retry(call)
		}
	}
}

func (c *Calls) retry(call Call) {
	time.Sleep(5 * time.Minute)
	_, _ = c.Place(context.Background(), PlaceRequest{BotID: call.BotID, Reason: call.Reason, RuleID: call.RuleID})
}

func (c *Calls) clear(l *live) {
	c.mu.Lock()
	if c.cur == l {
		c.cur = nil
	}
	c.mu.Unlock()
}
```

Add a small helper in the same file:

```go
func jsonRaw(v any) (json.RawMessage, error) { b, err := json.Marshal(v); return b, err }
```

(import `encoding/json`), and remove the no-op `if l.call.BotID == "" { text = text }` block if `go vet` flags it.

The `retry` goroutine uses a real sleep; inject `after func(time.Duration) <-chan time.Time` into `Calls` (default `time.After`) so tests can drive it, and add `TestUrgentRetryOnce` that sets `urgent`, delivers a `no-answer` status, fires the fake timer, and asserts exactly two dials.

- [ ] **Step 4: Log per-turn timings (spec §7)**

In `turn`, record `start := c.now()` on entry, the time of the first `say` call (wrap `OnText`/`OnTool` to capture it once), and the end; after `done` log
`c.log.Info("jarvis: turn", "call", l.call.ID, "first_audio_ms", first.Sub(start).Milliseconds(), "total_ms", end.Sub(start).Milliseconds(), "opener", isOpener)`.
No transcript text in logs. Add `TestTurnLogsTimingsWithoutText` using a `slog` handler that records attributes and asserts `first_audio_ms` is present and the utterance text is absent.

- [ ] **Step 5: Run the tests until they pass**

Run: `go test ./internal/jarvis/ -race -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/jarvis/calls.go internal/jarvis/calls_test.go
git commit -m "feat(jarvis): add call manager for outbound and inbound calls"
```

---

### Task 10: Jarvis service and daemon wiring

**Files:**
- Create: `internal/jarvis/service.go`, `internal/jarvis/service_test.go`, `internal/daemon/svc_jarvis.go`, `internal/daemon/svc_jarvis_test.go`
- Modify: `internal/daemon/daemon.go` (field `Jarvis *jarvis.Service`, start in `Run`, close in both cleanup paths), `internal/daemon/services.go` (`d.registerJarvis()` after `registerChatTools`), `internal/daemon/svc_chat_tools.go` (add `tools.Jarvis{Runtime: d.jarvisTools()}` and `m.AddPromptSection(tools.JarvisPrompt)`), `internal/daemon/app_events.go` (`taskEvents.publish` calls `d.Jarvis.OnEvent` for finished/failed), `internal/daemon/svc_automations.go` (`Changed` closure forwards terminal runs), `internal/daemon/svc_notifications.go` (`notifyResult` forwards stored notifications)

**Interfaces:**
- Consumes: everything above; daemon helpers `d.botToolAccess`, `d.botTaskDefaults`, `d.agentSettings`, `d.audit`, `d.Notify`, `credentials.Store`.
- Produces:
  - `type Service struct` with `func NewService(cfg ServiceConfig) *Service`; `ServiceConfig{DB *sql.DB; Creds credentials.Store; Host Host; Log *slog.Logger; Now func() time.Time; HTTP *http.Client; LoadSettings func() (Settings, error); SaveSettings func(Settings) error}`
  - `(*Service) Start(ctx)`, `Close()`, `OnEvent(ev Event)`, `Status(ctx) StatusView`, `SetSecrets(SecretsInput) error`, `Configure(ctx, Settings) (Settings, error)` (runs `Provision`), `GenerateRelaySecrets() (RelaySecrets, error)`, `StartVerification(ctx, number string) error`, `ConfirmVerification(code string) error`, `Voices(ctx)`, `Models(ctx)`, `Rules/AddRule/DeleteRule`, `RecentCalls`, `TestCall(ctx, botID)`
  - `type StatusView struct{Settings Settings; HasTwilio, HasElevenLabs, HasRelaySecrets, RelayConnected bool; CallsToday int; MonthCostMicros int64; LastError string}`
  - IPC methods (daemon): `jarvis.status`, `jarvis.secrets.set`, `jarvis.relaySecrets.generate`, `jarvis.configure`, `jarvis.verify.start`, `jarvis.verify.confirm`, `jarvis.voices`, `jarvis.models`, `jarvis.rules.list`, `jarvis.rules.delete`, `jarvis.calls.recent`, `jarvis.call.test`; GUI event `jarvis.changed` (payload `StatusView`)

- [ ] **Step 1: Write the failing service tests**

```go
package jarvis

import (
	"context"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

func newTestService(t *testing.T) (*Service, *fakeHost, *fakeDialer, *fakeSender, *time.Time) {
	st, now := testStore(t)
	*now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	host := newFakeHost()
	dial := &fakeDialer{}
	relay := &fakeSender{}
	settings := Settings{OwnerNumber: "+32470000000", OwnerVerified: true, AgentID: "ag", PhoneNumberID: "pn"}
	svc := newServiceForTest(st, credentials.NewMemory(), host, dial, relay, &settings, func() time.Time { return *now })
	return svc, host, dial, relay, now
}

func TestRuleEventPlacesOneCallAndDeletesOnceRule(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	ctx := context.Background()
	r, _ := svc.AddRule(ctx, Rule{BotID: "b1", Trigger: TaskFailed, Target: "t1", Note: "deploy"})
	svc.OnEvent(Event{Trigger: TaskFailed, TargetID: "t1", Title: "deploy-api failed"})
	svc.wait()
	if len(dial.calls) != 1 {
		t.Fatalf("dials=%d", len(dial.calls))
	}
	rules, _ := svc.Rules(ctx, "b1")
	if len(rules) != 0 {
		t.Fatalf("once rule %s kept", r.ID)
	}
}

func TestQuietHoursTurnRuleIntoNotice(t *testing.T) {
	svc, host, dial, _, now := newTestService(t)
	svc.settings.QuietStart, svc.settings.QuietEnd = "11:00", "13:00"
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: TaskFailed, Target: "any"})
	svc.OnEvent(Event{Trigger: TaskFailed, TargetID: "t1", OwnerBotID: "b1", Title: "x"})
	svc.wait()
	_ = now
	if len(dial.calls) != 0 || len(host.notices) != 1 {
		t.Fatalf("dials=%d notices=%v", len(dial.calls), host.notices)
	}
}

func TestMissedAtTimeBecomesNotice(t *testing.T) {
	svc, host, dial, _, now := newTestService(t)
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: AtTime, Target: "08:00"})
	svc.lastTick = now.Add(-6 * time.Hour) // daemon was down from 06:00 to 12:00
	svc.tick(context.Background())
	svc.wait()
	if len(dial.calls) != 0 || len(host.notices) != 1 {
		t.Fatalf("dials=%d notices=%v", len(dial.calls), host.notices)
	}
}

func TestAtTimeFiresOnTick(t *testing.T) {
	svc, _, dial, _, now := newTestService(t)
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: AtTime, Target: "12:01", Repeat: true})
	svc.lastTick = *now
	*now = now.Add(90 * time.Second)
	svc.tick(context.Background())
	svc.wait()
	if len(dial.calls) != 1 {
		t.Fatalf("dials=%d", len(dial.calls))
	}
}

func TestCallOwnerRespectsCoolDown(t *testing.T) {
	svc, _, dial, _, now := newTestService(t)
	ctx := context.Background()
	if _, err := svc.CallOwner(ctx, "b1", "first", false); err != nil {
		t.Fatal(err)
	}
	svc.calls.HandleFrame(ctx, Frame{Type: FrameCallStatus, CallID: dial.calls[0].CallID, Status: "done"})
	*now = now.Add(5 * time.Minute)
	if _, err := svc.CallOwner(ctx, "b1", "second", false); err == nil {
		t.Fatal("cool-down ignored")
	}
}

func TestVerificationFlow(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	svc.settings.OwnerVerified = false
	if err := svc.StartVerification(context.Background(), "+32471111111"); err != nil {
		t.Fatal(err)
	}
	if len(dial.calls) != 1 || dial.calls[0].To != "+32471111111" {
		t.Fatalf("dials %+v", dial.calls)
	}
	if svc.ConfirmVerification("000000") == nil {
		t.Fatal("wrong code accepted")
	}
	if err := svc.ConfirmVerification(svc.pendingCode); err != nil || !svc.settings.OwnerVerified || svc.settings.OwnerNumber != "+32471111111" {
		t.Fatalf("err=%v settings=%+v", err, svc.settings)
	}
}
```

`newServiceForTest`, `wait`, `tick`, `lastTick`, `pendingCode` and `settings` are unexported members used only by tests; define them in `service.go`.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/jarvis/ -run 'TestRuleEvent|TestQuietHours|TestMissedAtTime|TestAtTime|TestCallOwner|TestVerification'`
Expected: FAIL, undefined `Service`.

- [ ] **Step 3: Implement `service.go`**

```go
package jarvis

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

var e164 = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type ServiceConfig struct {
	DB           *sql.DB
	Creds        credentials.Store
	Host         Host
	Log          *slog.Logger
	Now          func() time.Time
	HTTP         *http.Client
	LoadSettings func() (Settings, error)
	SaveSettings func(Settings) error
	Changed      func(StatusView)
}

type StatusView struct {
	Settings        Settings `json:"settings"`
	HasTwilio       bool     `json:"hasTwilio"`
	HasElevenLabs   bool     `json:"hasElevenLabs"`
	HasRelaySecrets bool     `json:"hasRelaySecrets"`
	RelayConnected  bool     `json:"relayConnected"`
	CallsToday      int      `json:"callsToday"`
	MonthCostMicros int64    `json:"monthCostMicros"`
	LastError       string   `json:"lastError,omitempty"`
}

type SecretsInput struct {
	TwilioSID   string `json:"twilioSid,omitempty"`
	TwilioToken string `json:"twilioToken,omitempty"`
	ElevenLabs  string `json:"elevenLabsKey,omitempty"`
}

// RelaySecrets are shown to the owner once so they can `wrangler secret put` them.
type RelaySecrets struct {
	RelayToken    string `json:"relayToken"`
	LLMSecret     string `json:"llmSecret"`
	WebhookSecret string `json:"webhookSecret"`
}

type Service struct {
	cfg   ServiceConfig
	store *Store
	calls *Calls
	relay *RelayClient
	el    *ElevenLabs
	log   *slog.Logger
	now   func() time.Time

	mu          sync.Mutex
	settings    Settings
	lastErr     string
	pendingCode string
	pendingNum  string
	lastTick    time.Time
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	relayCancel context.CancelFunc
}

func NewService(cfg ServiceConfig) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Service{cfg: cfg, store: NewStore(cfg.DB, cfg.Now), log: cfg.Log, now: cfg.Now}
	if cfg.LoadSettings != nil {
		if v, err := cfg.LoadSettings(); err == nil {
			s.settings = v
		}
	}
	s.el = &ElevenLabs{HTTP: cfg.HTTP}
	s.relay = NewRelayClient("", "", func(f Frame) { s.calls.HandleFrame(context.Background(), f) }, cfg.Log)
	s.calls = NewCalls(s.store, cfg.Host, (*keyedEL)(s), s.relay, s.Settings, cfg.Now, cfg.Log)
	return s
}

// keyedEL reads the ElevenLabs key from Credential Manager on every use, so a key change applies at once.
type keyedEL Service

func (k *keyedEL) client() (*ElevenLabs, error) {
	key, err := k.cfg.Creds.Get(KeyElevenLabs)
	if err != nil {
		return nil, &Refusal{"The ElevenLabs API key is not set."}
	}
	return &ElevenLabs{Key: string(key), BaseURL: k.el.BaseURL, HTTP: k.el.HTTP}, nil
}

func (k *keyedEL) OutboundCall(ctx context.Context, c OutboundCall) (string, string, error) {
	el, err := k.client()
	if err != nil {
		return "", "", err
	}
	return el.OutboundCall(ctx, c)
}

func (k *keyedEL) ConversationStatus(ctx context.Context, id string) (string, int, error) {
	el, err := k.client()
	if err != nil {
		return "", 0, err
	}
	return el.ConversationStatus(ctx, id)
}

func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

func (s *Service) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.lastTick = s.now()
	s.restartRelay(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.tick(ctx)
			}
		}
	}()
}

func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Service) wait() { s.wg.Wait() }

func (s *Service) restartRelay(ctx context.Context) {
	if s.relayCancel != nil {
		s.relayCancel()
	}
	set := s.Settings()
	tok, err := s.cfg.Creds.Get(KeyRelayToken)
	if set.RelayURL == "" || err != nil {
		return
	}
	url := "wss://" + trimScheme(set.RelayURL) + "/daemon"
	rc := NewRelayClient(url, string(tok), func(f Frame) { s.calls.HandleFrame(ctx, f) }, s.log)
	s.relay.url, s.relay.token = rc.url, rc.token
	rctx, cancel := context.WithCancel(ctx)
	s.relayCancel = cancel
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.relay.Run(rctx) }()
}

func trimScheme(u string) string {
	for _, p := range []string{"https://", "http://", "wss://", "ws://"} {
		if len(u) > len(p) && u[:len(p)] == p {
			u = u[len(p):]
		}
	}
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}

func (s *Service) usage(ctx context.Context, botID string) Usage {
	now := s.now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UnixMilli()
	today, _, _ := s.store.CallsSince(ctx, day)
	_, cost, _ := s.store.CallsSince(ctx, month)
	last, _ := s.store.LastCallAt(ctx, botID)
	_, in := s.calls.Active()
	return Usage{Today: today, MonthCostMicros: cost, LastBotCallMs: last, InCall: in}
}

// fire places a call for a matched rule or a judgement call, or explains in a notification why it did not.
func (s *Service) fire(ctx context.Context, botID, reason, brief, ruleID string, ask Ask, urgent bool) (Call, error) {
	u := s.usage(ctx, botID)
	if u.InCall {
		_, err := s.calls.Place(ctx, PlaceRequest{BotID: botID, Reason: reason})
		return Call{}, err // ErrInjected
	}
	if err := Allow(s.Settings(), s.now(), u, ask); err != nil {
		bot, _ := s.cfg.Host.Bot(ctx, botID)
		s.cfg.Host.Notify(bot.Name+" did not call", reason+" ("+err.Error()+")", "bot:"+botID, "info")
		return Call{}, err
	}
	return s.calls.Place(ctx, PlaceRequest{BotID: botID, Reason: reason, Brief: brief, RuleID: ruleID, Urgent: urgent})
}

// OnEvent matches rules against a task, automation or notification event. It never blocks the caller.
func (s *Service) OnEvent(ev Event) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx := context.Background()
		rules, err := s.store.Rules(ctx, "")
		if err != nil {
			return
		}
		nowMs := s.now().UnixMilli()
		for _, r := range rules {
			if Expired(r, nowMs) {
				_ = s.store.DeleteRule(ctx, r.ID)
				continue
			}
			if !Matches(r, ev, nowMs) {
				continue
			}
			if !r.Repeat {
				_ = s.store.DeleteRule(ctx, r.ID)
			}
			brief := "Why you are calling: " + ev.Title
			if r.Note != "" {
				brief += "\nThe owner's note for this rule: " + chat.Fence("rule note", r.Note)
			}
			if ev.Detail != "" {
				brief += "\nDetails:\n" + chat.Fence("event detail", ev.Detail)
			}
			_, _ = s.fire(ctx, r.BotID, ev.Title, brief, r.ID, Ask{QuietOverride: r.QuietOverride}, r.Urgent)
		}
	}()
}

// tick fires at_time rules whose time passed since the last tick. Times missed by more than 2 minutes
// (daemon asleep or down) become a notification instead of a late call.
func (s *Service) tick(ctx context.Context) {
	now := s.now()
	prev := s.lastTick
	s.lastTick = now
	rules, err := s.store.Rules(ctx, "")
	if err != nil {
		return
	}
	for _, r := range rules {
		if r.Trigger != AtTime {
			continue
		}
		at := NextAt(r, prev)
		if at.IsZero() || at.After(now) {
			continue
		}
		if !r.Repeat {
			_ = s.store.DeleteRule(ctx, r.ID)
		}
		reason := "It's " + r.Target
		if r.Note != "" {
			reason = r.Note
		}
		if now.Sub(at) > 2*time.Minute {
			bot, _ := s.cfg.Host.Bot(ctx, r.BotID)
			s.cfg.Host.Notify(bot.Name+" missed a scheduled call", reason, "bot:"+r.BotID, "info")
			continue
		}
		r := r
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_, _ = s.fire(context.Background(), r.BotID, reason, "Scheduled call at "+r.Target+".", r.ID, Ask{QuietOverride: r.QuietOverride}, r.Urgent)
		}()
	}
}

func (s *Service) CallOwner(ctx context.Context, botID, reason string, urgent bool) (string, error) {
	_, err := s.fire(ctx, botID, reason, "Why you are calling: "+reason, "", Ask{Judgement: true}, urgent)
	if errors.Is(err, ErrInjected) {
		return "You are already on a call; the owner will hear this there.", nil
	}
	if err != nil {
		return "", err
	}
	return "Calling the owner now.", nil
}

func (s *Service) AddRule(ctx context.Context, r Rule) (Rule, error) { return s.store.AddRule(ctx, r) }
func (s *Service) Rules(ctx context.Context, botID string) ([]Rule, error) {
	return s.store.Rules(ctx, botID)
}
func (s *Service) DeleteRule(ctx context.Context, id string) error { return s.store.DeleteRule(ctx, id) }
func (s *Service) RecentCalls(ctx context.Context, botID string) ([]Call, error) {
	return s.store.RecentCalls(ctx, botID, 20)
}
func (s *Service) SwitchBot(ctx context.Context, callID, name string) (string, error) {
	return s.calls.SwitchBot(ctx, callID, name)
}

func (s *Service) Status(ctx context.Context) StatusView {
	set := s.Settings()
	has := func(k string) bool { _, err := s.cfg.Creds.Get(k); return err == nil }
	u := s.usage(ctx, "")
	s.mu.Lock()
	last := s.lastErr
	s.mu.Unlock()
	return StatusView{Settings: set, HasTwilio: has(KeyTwilioSID) && has(KeyTwilioToken), HasElevenLabs: has(KeyElevenLabs),
		HasRelaySecrets: has(KeyRelayToken) && has(KeyLLMSecret) && has(KeyWebhookSecret), RelayConnected: s.relay.Connected(),
		CallsToday: u.Today, MonthCostMicros: u.MonthCostMicros, LastError: last}
}

func (s *Service) SetSecrets(in SecretsInput) error {
	for k, v := range map[string]string{KeyTwilioSID: in.TwilioSID, KeyTwilioToken: in.TwilioToken, KeyElevenLabs: in.ElevenLabs} {
		if v == "" {
			continue
		}
		if err := s.cfg.Creds.Set(k, []byte(v)); err != nil {
			return err
		}
	}
	return nil
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) GenerateRelaySecrets() (RelaySecrets, error) {
	var out RelaySecrets
	for _, p := range []struct {
		key string
		dst *string
	}{{KeyRelayToken, &out.RelayToken}, {KeyLLMSecret, &out.LLMSecret}, {KeyWebhookSecret, &out.WebhookSecret}} {
		v, err := randomSecret()
		if err != nil {
			return out, err
		}
		if err := s.cfg.Creds.Set(p.key, []byte(v)); err != nil {
			return out, err
		}
		*p.dst = v
	}
	return out, nil
}

// Configure saves the plain settings, provisions ElevenLabs and reconnects the relay.
func (s *Service) Configure(ctx context.Context, in Settings) (Settings, error) {
	if in.TwilioNumber != "" && !e164.MatchString(in.TwilioNumber) {
		return in, &Refusal{"The Jarvis number must look like +32470123456."}
	}
	if in.QuietStart != "" && !hhmm.MatchString(in.QuietStart) || in.QuietEnd != "" && !hhmm.MatchString(in.QuietEnd) {
		return in, &Refusal{"Quiet hours must look like 22:00."}
	}
	cur := s.Settings()
	in.OwnerNumber, in.OwnerVerified, in.AgentID, in.PhoneNumberID = cur.OwnerNumber, cur.OwnerVerified, cur.AgentID, cur.PhoneNumberID
	if in.RelayURL != "" && in.TwilioNumber != "" {
		el, err := (*keyedEL)(s).client()
		if err != nil {
			return in, err
		}
		sid, err1 := s.cfg.Creds.Get(KeyTwilioSID)
		tok, err2 := s.cfg.Creds.Get(KeyTwilioToken)
		llmSecret, err3 := s.cfg.Creds.Get(KeyLLMSecret)
		if err1 != nil || err2 != nil || err3 != nil {
			return in, &Refusal{"Add the Twilio credentials and generate the relay secrets first."}
		}
		tw := &Twilio{SID: string(sid), Token: string(tok), HTTP: s.cfg.HTTP}
		if in, err = Provision(ctx, el, tw, in, ProvisionSecrets{LLMSecret: string(llmSecret), TwilioSID: string(sid), TwilioToken: string(tok)}); err != nil {
			s.mu.Lock()
			s.lastErr = err.Error()
			s.mu.Unlock()
			return in, err
		}
	}
	if s.cfg.SaveSettings != nil {
		if err := s.cfg.SaveSettings(in); err != nil {
			return in, err
		}
	}
	s.mu.Lock()
	s.settings, s.lastErr = in, ""
	s.mu.Unlock()
	if s.cancel != nil {
		s.restartRelay(context.Background())
	}
	return in, nil
}

func (s *Service) StartVerification(ctx context.Context, number string) error {
	if !e164.MatchString(number) {
		return &Refusal{"Enter your number like +32470123456."}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	s.mu.Lock()
	s.pendingCode, s.pendingNum = code, number
	prev := s.settings
	s.settings.OwnerNumber = number // the call goes to the number being verified
	s.mu.Unlock()
	bots, _ := s.cfg.Host.Bots(ctx)
	if len(bots) == 0 {
		return &Refusal{"Create a bot first."}
	}
	_, err = s.calls.Place(ctx, PlaceRequest{BotID: bots[0].ID, Reason: "phone verification", Verify: code})
	if err != nil {
		s.mu.Lock()
		s.settings = prev
		s.mu.Unlock()
	}
	return err
}

func (s *Service) ConfirmVerification(code string) error {
	s.mu.Lock()
	if s.pendingCode == "" || code != s.pendingCode {
		s.mu.Unlock()
		return &Refusal{"That code is not right."}
	}
	s.settings.OwnerNumber, s.settings.OwnerVerified = s.pendingNum, true
	s.pendingCode, s.pendingNum = "", ""
	set := s.settings
	s.mu.Unlock()
	if s.cfg.SaveSettings != nil {
		return s.cfg.SaveSettings(set)
	}
	return nil
}

func (s *Service) Voices(ctx context.Context) ([]Voice, error) {
	el, err := (*keyedEL)(s).client()
	if err != nil {
		return nil, err
	}
	return el.Voices(ctx)
}

func (s *Service) Models(ctx context.Context) ([]TTSModel, error) {
	el, err := (*keyedEL)(s).client()
	if err != nil {
		return nil, err
	}
	return el.Models(ctx)
}

// newServiceForTest builds a Service around fakes without network or relay goroutines.
func newServiceForTest(st *Store, creds credentials.Store, host Host, dial Dialer, relay Sender, settings *Settings, now func() time.Time) *Service {
	s := &Service{cfg: ServiceConfig{Creds: creds, Host: host, Now: now}, store: st, log: slog.Default(), now: now, settings: *settings,
		relay: NewRelayClient("", "", func(Frame) {}, slog.Default())}
	s.calls = NewCalls(st, host, dial, relay, s.Settings, now, slog.Default())
	return s
}
```

Note: the test helper passes `relay` (a `fakeSender`) to `NewCalls` so `Place` sees it as connected; `Status` uses `s.relay` and is not exercised by these tests.

- [ ] **Step 4: Run the service tests until they pass**

Run: `go test ./internal/jarvis/ -race -v`
Expected: PASS.

- [ ] **Step 5: Write the failing daemon test**

`internal/daemon/svc_jarvis_test.go`, using the daemon's existing test constructor (find it with `grep -n "func newTestDaemon\|func testDaemon" internal/daemon/*_test.go` and use that name):

```go
func TestJarvisSaveCheckRequiresFullPermissions(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()
	b, err := d.Chat.SaveBot(ctx, chat.BotInput{Name: "Atlas"})
	if err != nil {
		t.Fatal(err)
	}
	in := chat.BotInput{ID: b.ID, Name: "Atlas", Config: chat.BotConfig{CodeTasks: true, ToolAccess: chat.ToolAccessAll,
		Tasks: chat.BotTaskDefaults{PermissionMode: "yolo"}, Jarvis: chat.BotJarvis{Enabled: true}}}
	if _, err := d.Chat.SaveBot(ctx, in); err == nil || !strings.Contains(err.Error(), "YOLO") {
		t.Fatalf("saved without agents.botYolo: %v", err)
	}
	if _, err := d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents','{"botYolo":true}',0)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Chat.SaveBot(ctx, in); err != nil {
		t.Fatalf("full permissions rejected: %v", err)
	}
}

func TestJarvisDisableClearsFlag(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()
	_, _ = d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents','{"botYolo":true}',0)`)
	b, _ := d.Chat.SaveBot(ctx, chat.BotInput{Name: "Atlas", Config: chat.BotConfig{CodeTasks: true, ToolAccess: chat.ToolAccessAll,
		Tasks: chat.BotTaskDefaults{PermissionMode: "yolo"}, Jarvis: chat.BotJarvis{Enabled: true}}})
	h := jarvisHost{d: d}
	h.DisableJarvis(ctx, b.ID, "tool access lowered")
	got, _ := d.Chat.Bot(ctx, b.ID)
	if got.Config.Jarvis.Enabled {
		t.Fatal("still enabled")
	}
}
```

- [ ] **Step 6: Run it and see it fail**

Run: `go test ./internal/daemon/ -run TestJarvis`
Expected: FAIL, undefined `jarvisHost`.

- [ ] **Step 7: Implement `svc_jarvis.go`**

```go
package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/jarvis"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

// jarvisHost adapts the daemon to jarvis.Host.
type jarvisHost struct{ d *Daemon }

func (h jarvisHost) Bot(ctx context.Context, id string) (chat.Bot, error) { return h.d.Chat.Bot(ctx, id) }
func (h jarvisHost) Bots(ctx context.Context) ([]chat.Bot, error)        { return h.d.Chat.Bots(ctx) }

func (h jarvisHost) Gate(ctx context.Context, b chat.Bot) error {
	if !b.Config.Jarvis.Enabled {
		return &jarvis.GateError{Reason: "Jarvis mode is off for " + b.Name + "."}
	}
	return jarvis.CheckGate(h.d.gateInput(ctx, b.Config))
}

func (h jarvisHost) VoiceTurn(ctx context.Context, r chat.VoiceRequest) (string, error) { return h.d.Chat.VoiceTurn(ctx, r) }
func (h jarvisHost) Compose(ctx context.Context, botID, in string) (string, error)     { return h.d.Chat.Compose(ctx, botID, in) }
func (h jarvisHost) PostCallCard(ctx context.Context, botID string, p chat.MsgPart) (string, error) {
	return h.d.Chat.PostCallCard(ctx, botID, p)
}
func (h jarvisHost) UpdateCallCard(ctx context.Context, id string, p chat.MsgPart) error {
	return h.d.Chat.UpdateCallCard(ctx, id, p)
}

func (h jarvisHost) Notify(title, body, target, level string) {
	h.d.notices.notify(notifications.Input{Source: "jarvis", Kind: notifications.Message, Title: title, Body: body, Level: notifications.Level(level), Target: target})
}

func (h jarvisHost) Audit(action, target, outcome, detail string) { h.d.audit(action, target, outcome, detail) }

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

// gateInput resolves the bot's effective permissions the same way tool calls and task starts do.
func (d *Daemon) gateInput(ctx context.Context, c chat.BotConfig) jarvis.GateInput {
	v, ok := d.agentSettings()
	access := c.ToolAccess
	if access == "" {
		access = d.globalBotToolAccess()
	}
	return jarvis.GateInput{ToolAccess: access, TaskMode: d.botTaskDefaults(c.Tasks).PermissionMode,
		BotYolo: ok && v.BotYolo, YoloDisabled: !ok || v.YoloDisabled, CodeTasks: c.CodeTasks}
}

// jarvisTools adapts the service to tools.JarvisRuntime; bots may only touch their own rules.
type jarvisTools struct{ d *Daemon }

func (t jarvisTools) AddRule(ctx context.Context, botID string, r tools.JarvisRule) (tools.JarvisRule, error) {
	got, err := t.d.Jarvis.AddRule(ctx, jarvis.Rule{BotID: botID, Trigger: jarvis.Trigger(r.Trigger), Target: r.Target, Note: r.Note,
		Repeat: r.Repeat, Urgent: r.Urgent, QuietOverride: r.QuietOverride, ExpiresAt: r.ExpiresAt})
	if err == nil {
		t.d.jarvisChanged()
	}
	return toToolRule(got), err
}

func (t jarvisTools) Rules(ctx context.Context, botID string) ([]tools.JarvisRule, error) {
	rs, err := t.d.Jarvis.Rules(ctx, botID)
	out := make([]tools.JarvisRule, 0, len(rs))
	for _, r := range rs {
		out = append(out, toToolRule(r))
	}
	return out, err
}

func (t jarvisTools) DeleteRule(ctx context.Context, botID, id string) error {
	rs, err := t.d.Jarvis.Rules(ctx, botID)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.ID == id {
			err := t.d.Jarvis.DeleteRule(ctx, id)
			t.d.jarvisChanged()
			return err
		}
	}
	return errors.New("You have no rule with that id.")
}

func (t jarvisTools) CallOwner(ctx context.Context, botID, reason string, urgent bool) (string, error) {
	return t.d.Jarvis.CallOwner(ctx, botID, reason, urgent)
}

func (t jarvisTools) SwitchBot(ctx context.Context, callID, name string) (string, error) {
	return t.d.Jarvis.SwitchBot(ctx, callID, name)
}

func toToolRule(r jarvis.Rule) tools.JarvisRule {
	return tools.JarvisRule{ID: r.ID, Trigger: string(r.Trigger), Target: r.Target, Note: r.Note, Repeat: r.Repeat, Urgent: r.Urgent,
		QuietOverride: r.QuietOverride, ExpiresAt: r.ExpiresAt}
}

func (d *Daemon) loadJarvisSettings() (jarvis.Settings, error) {
	var raw string
	var s jarvis.Settings
	err := d.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, jarvis.SettingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal([]byte(raw), &s)
}

func (d *Daemon) saveJarvisSettings(s jarvis.Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,unixepoch()) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		jarvis.SettingsKey, string(b))
	return err
}

func (d *Daemon) jarvisChanged() {
	d.IPC.Publish("jarvis.changed", d.Jarvis.Status(context.Background()))
}

func jarvisErr(err error) error {
	var r *jarvis.Refusal
	var g *jarvis.GateError
	if errors.As(err, &r) || errors.As(err, &g) {
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	}
	return err
}

func (d *Daemon) registerJarvis() {
	d.Jarvis = jarvis.NewService(jarvis.ServiceConfig{DB: d.DB.DB, Creds: credentials.New(), Host: jarvisHost{d}, Log: d.Log,
		HTTP: llm.NewHTTPClient(), LoadSettings: d.loadJarvisSettings, SaveSettings: d.saveJarvisSettings})
	d.Chat.SetSaveCheck(func(ctx context.Context, in chat.BotInput) error {
		if err := jarvis.CheckGate(d.gateInput(ctx, in.Config)); err != nil {
			return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
		}
		d.audit("jarvis.enabled", in.ID, "allowed", in.Name)
		return nil
	})
	handle(d, "jarvis.status", func(ctx context.Context, _ none) (jarvis.StatusView, error) { return d.Jarvis.Status(ctx), nil })
	handle(d, "jarvis.secrets.set", func(ctx context.Context, p jarvis.SecretsInput) (jarvis.StatusView, error) {
		if err := d.Jarvis.SetSecrets(p); err != nil {
			return jarvis.StatusView{}, err
		}
		d.jarvisChanged()
		return d.Jarvis.Status(ctx), nil
	})
	handle(d, "jarvis.relaySecrets.generate", func(ctx context.Context, _ none) (jarvis.RelaySecrets, error) {
		out, err := d.Jarvis.GenerateRelaySecrets()
		d.audit("jarvis.relay.secrets", "", "allowed", "generated")
		return out, err
	})
	handle(d, "jarvis.configure", func(ctx context.Context, p jarvis.Settings) (jarvis.StatusView, error) {
		if _, err := d.Jarvis.Configure(ctx, p); err != nil {
			return jarvis.StatusView{}, jarvisErr(err)
		}
		d.jarvisChanged()
		return d.Jarvis.Status(ctx), nil
	})
	handle(d, "jarvis.verify.start", func(ctx context.Context, p struct{ Number string `json:"number"` }) (none, error) {
		return none{}, jarvisErr(d.Jarvis.StartVerification(ctx, p.Number))
	})
	handle(d, "jarvis.verify.confirm", func(ctx context.Context, p struct{ Code string `json:"code"` }) (jarvis.StatusView, error) {
		if err := d.Jarvis.ConfirmVerification(p.Code); err != nil {
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
	handle(d, "jarvis.rules.list", func(ctx context.Context, p struct{ BotID string `json:"botId"` }) ([]jarvis.Rule, error) {
		return d.Jarvis.Rules(ctx, p.BotID)
	})
	handle(d, "jarvis.rules.delete", func(ctx context.Context, p struct{ ID string `json:"id"` }) (none, error) {
		err := d.Jarvis.DeleteRule(ctx, p.ID)
		d.jarvisChanged()
		return none{}, err
	})
	handle(d, "jarvis.calls.recent", func(ctx context.Context, p struct{ BotID string `json:"botId"` }) ([]jarvis.Call, error) {
		return d.Jarvis.RecentCalls(ctx, p.BotID)
	})
	handle(d, "jarvis.call.test", func(ctx context.Context, p struct{ BotID string `json:"botId"` }) (string, error) {
		msg, err := d.Jarvis.CallOwner(ctx, p.BotID, "a test call you started from settings", false)
		return msg, jarvisErr(err)
	})
}
```

Then:
- `daemon.go`: add `Jarvis *jarvis.Service` to `Daemon`; in `Run` after `d.Automations.Start(ctx)` add `d.Jarvis.Start(ctx)`; in both cleanup paths add `d.Jarvis.Close()` before `d.Chat.Close()` (guard with `if d.Jarvis != nil`).
- `services.go`: add `d.registerJarvis()` right after `d.registerChatTools()`.
- `svc_chat_tools.go`: append `tools.Jarvis{Runtime: jarvisTools{d}}` to the tool list and call `m.AddPromptSection(tools.JarvisPrompt)`. `registerChatTools` runs before `registerJarvis`, so `jarvisTools` must read `d.Jarvis` lazily (it does: it dereferences at call time).
- `app_events.go` `taskEvents.publish`: after `t.owned(topic, s)` add a callback `t.jarvis(topic, s)`; wire it in `newTaskEvents` from `svc_agents.go` as:

```go
func(topic string, s agents.Session) {
	if d.Jarvis == nil {
		return
	}
	trig := map[string]jarvis.Trigger{events.TopicAgentFinished: jarvis.TaskFinished, events.TopicAgentFailed: jarvis.TaskFailed}[topic]
	if trig == "" {
		return
	}
	d.Jarvis.OnEvent(jarvis.Event{Trigger: trig, TargetID: s.ID, OwnerBotID: strings.TrimPrefix(s.Owner, chat.BotPrefix),
		Title: "Task " + s.Title + map[jarvis.Trigger]string{jarvis.TaskFinished: " finished", jarvis.TaskFailed: " failed"}[trig],
		Detail: taskReportText(topic, s)})
}
```

  (check `agents.Session` field names `ID`, `Title`, `Owner` and adjust).
- `svc_automations.go` `Changed` closure: after `d.notices.run(r)` add

```go
if d.Jarvis != nil && (r.Status == "succeeded" || r.Status == "failed") {
	trig := jarvis.AutomationFinished
	if r.Status == "failed" {
		trig = jarvis.AutomationFailed
	}
	d.Jarvis.OnEvent(jarvis.Event{Trigger: trig, TargetID: r.AutomationID, Title: "Automation " + r.Name + " " + r.Status, Detail: r.Error})
}
```

  (`Changed` fires on every journal change; `OnEvent` must only see terminal transitions once. Keep a `sync.Map` of run ids already forwarded in the closure, deleting entries older than an hour on each insert.)
- `svc_notifications.go` `notifyResult`: after a stored result, if `in.Source != "jarvis"` (never react to our own notices) call `d.Jarvis.OnEvent(jarvis.Event{Trigger: jarvis.NotificationTrigger, TargetID: string(in.Kind), Source: in.Source, Title: in.Title, Detail: in.Body})`.

- [ ] **Step 8: Run all Go checks**

Run: `go fmt ./... && go vet ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/jarvis/ internal/daemon/
git commit -m "feat(daemon): wire jarvis service, ipc and event hooks"
```

---

### Task 11: Desktop service and bindings

**Files:**
- Create: `internal/app/jarvis.go`
- Modify: `internal/app/bridge.go` (`forward` chain, long-running methods), `cmd/desktop/main.go` (bind), `frontend/src/mygo.ts` (generated)

**Interfaces:**
- Consumes: daemon IPC methods from Task 10.
- Produces (MyGo service `Jarvis`): `Status()`, `SetSecrets(in)`, `GenerateRelaySecrets()`, `Configure(s)`, `StartVerification(number)`, `ConfirmVerification(code)`, `Voices()`, `Models()`, `Rules(botID)`, `DeleteRule(id)`, `RecentCalls(botID)`, `TestCall(botID)`; event `JarvisChanged` (`"jarvis:changed"`, `jarvis.StatusView`).

- [ ] **Step 1: Implement `internal/app/jarvis.go`**

```go
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

func (s Jarvis) Configure(ctx context.Context, in jarvis.Settings) (jarvis.StatusView, error) {
	var out jarvis.StatusView
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

func (s Jarvis) Rules(ctx context.Context, botID string) ([]jarvis.Rule, error) {
	var out []jarvis.Rule
	return out, s.L.call(ctx, "jarvis.rules.list", map[string]string{"botId": botID}, &out)
}

func (s Jarvis) DeleteRule(ctx context.Context, id string) error {
	return s.L.call(ctx, "jarvis.rules.delete", map[string]string{"id": id}, nil)
}

func (s Jarvis) RecentCalls(ctx context.Context, botID string) ([]jarvis.Call, error) {
	var out []jarvis.Call
	return out, s.L.call(ctx, "jarvis.calls.recent", map[string]string{"botId": botID}, &out)
}

func (s Jarvis) TestCall(ctx context.Context, botID string) (string, error) {
	var out string
	return out, s.L.call(ctx, "jarvis.call.test", map[string]string{"botId": botID}, &out)
}
```

- [ ] **Step 2: Wire it**

- `bridge.go` `forward`: add `forwardJarvis(event, data) ||` to the chain.
- `bridge.go` long-running switch (lines ~193-199): add `"jarvis.configure", "jarvis.verify.start", "jarvis.call.test"` (provisioning and dialing can exceed 30 s).
- `cmd/desktop/main.go`: add `mygo.Bind(app.Jarvis{L: link})` next to `app.Notifications`.

- [ ] **Step 3: Generate and build**

Run: `go tool mygo generate && go build ./... && bun run --cwd frontend check`
Expected: `frontend/src/mygo.ts` gains `export const Jarvis = {…}`, `events.jarvisChanged`, types `StatusView`, `Settings` (jarvis), `Rule`, `Call`, `Voice`, `TTSModel`, `BotJarvis`; build and checks pass. If the generated type name `Settings` clashes with an existing export, rename the Go type to `jarvis.PhoneSettings` everywhere and regenerate.

- [ ] **Step 4: Commit**

```bash
git add internal/app/jarvis.go internal/app/bridge.go cmd/desktop/main.go frontend/src/mygo.ts
git commit -m "feat(app): expose jarvis service to the desktop"
```

---

### Task 12: Settings › Phone page

**Files:**
- Create: `frontend/src/features/jarvis/store.ts`, `frontend/src/features/jarvis/PhoneSettings.tsx`, `frontend/src/features/jarvis/PhoneSettings.test.tsx`
- Modify: `frontend/src/features/settings/SettingsView.tsx` (section `{ id: "phone", label: "Phone", icon: Phone }` after "Voice", render line)

**Interfaces:**
- Consumes: `Jarvis.*`, `events.jarvisChanged` from `@/mygo`; `SettingsSection`, `Field` from settings; `Failure`, `Loading` from `@/features/mcp/shared`; `errText` from `@/features/agents/format`; `inDesktop` from `@/lib/backend`.
- Produces: `useJarvis` store `{status: StatusView | null; error: string | null; load(); startSync(): () => void; apply(v: StatusView)}`.

- [ ] **Step 1: Write the failing test**

```tsx
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  status: vi.fn(), setSecrets: vi.fn(), generateRelaySecrets: vi.fn(), configure: vi.fn(),
  startVerification: vi.fn(), confirmVerification: vi.fn(), voices: vi.fn(), models: vi.fn(),
  changed: [] as ((v: unknown) => void)[],
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Jarvis: {
    status: api.status, setSecrets: api.setSecrets, generateRelaySecrets: api.generateRelaySecrets, configure: api.configure,
    startVerification: api.startVerification, confirmVerification: api.confirmVerification, voices: api.voices, models: api.models,
  },
  events: { jarvisChanged: { on: (cb: (v: unknown) => void) => { api.changed.push(cb); return () => undefined; } } },
}));

import { PhoneSettings } from "./PhoneSettings";
import { useJarvis } from "./store";

const empty = { settings: {}, hasTwilio: false, hasElevenLabs: false, hasRelaySecrets: false, relayConnected: false, callsToday: 0, monthCostMicros: 0 };

beforeEach(() => {
  vi.clearAllMocks();
  useJarvis.setState({ status: null, error: null });
  api.voices.mockResolvedValue([]);
  api.models.mockResolvedValue([]);
});
afterEach(cleanup);

it("shows the setup checklist with nothing configured", async () => {
  api.status.mockResolvedValue(empty);
  render(<PhoneSettings />);
  expect(await screen.findByText("Twilio account")).toBeInTheDocument();
  expect(screen.getByText("Relay connected")).toBeInTheDocument();
});

it("saves Twilio secrets without echoing them back", async () => {
  api.status.mockResolvedValue(empty);
  api.setSecrets.mockResolvedValue({ ...empty, hasTwilio: true });
  render(<PhoneSettings />);
  fireEvent.change(await screen.findByLabelText("Account SID"), { target: { value: "AC123" } });
  fireEvent.change(screen.getByLabelText("Auth token"), { target: { value: "tok" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Twilio" }));
  await waitFor(() => expect(api.setSecrets).toHaveBeenCalledWith({ twilioSid: "AC123", twilioToken: "tok" }));
  expect(screen.getByLabelText("Auth token")).toHaveValue("");
});

it("shows generated relay secrets once with copy buttons", async () => {
  api.status.mockResolvedValue(empty);
  api.generateRelaySecrets.mockResolvedValue({ relayToken: "rt", llmSecret: "ls", webhookSecret: "ws" });
  render(<PhoneSettings />);
  fireEvent.click(await screen.findByRole("button", { name: "Generate relay secrets" }));
  expect(await screen.findByText("rt")).toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: /Copy/ })).toHaveLength(3);
});

it("verifies the owner number with a code", async () => {
  api.status.mockResolvedValue({ ...empty, hasTwilio: true, hasElevenLabs: true, hasRelaySecrets: true, relayConnected: true, settings: { agentId: "ag" } });
  api.startVerification.mockResolvedValue(undefined);
  api.confirmVerification.mockResolvedValue({ ...empty, settings: { ownerNumber: "+32470000000", ownerVerified: true } });
  render(<PhoneSettings />);
  fireEvent.change(await screen.findByLabelText("Your phone number"), { target: { value: "+32470000000" } });
  fireEvent.click(screen.getByRole("button", { name: "Call me with a code" }));
  await waitFor(() => expect(api.startVerification).toHaveBeenCalledWith("+32470000000"));
  fireEvent.change(await screen.findByLabelText("Code"), { target: { value: "123456" } });
  fireEvent.click(screen.getByRole("button", { name: "Verify" }));
  await waitFor(() => expect(api.confirmVerification).toHaveBeenCalledWith("123456"));
});

it("shows a daemon error", async () => {
  api.status.mockRejectedValue(new Error("daemon offline"));
  render(<PhoneSettings />);
  expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("daemon offline");
});
```

- [ ] **Step 2: Run it and see it fail**

Run: `bun run --cwd frontend vitest run src/features/jarvis/PhoneSettings.test.tsx`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `store.ts`**

```ts
import { create } from "zustand";
import { errText } from "@/features/agents/format";
import { inDesktop } from "@/lib/backend";
import { events, Jarvis, type StatusView } from "@/mygo";

interface JarvisStore {
  status: StatusView | null;
  error: string | null;
  load: () => Promise<void>;
  apply: (v: StatusView) => void;
  startSync: () => () => void;
}

export const useJarvis = create<JarvisStore>((set) => ({
  status: null,
  error: null,
  load: async () => {
    if (!inDesktop) return;
    try {
      set({ status: await Jarvis.status(), error: null });
    } catch (failure) {
      set({ error: errText(failure) });
    }
  },
  apply: (status) => set({ status, error: null }),
  startSync: () => events.jarvisChanged.on((v) => set({ status: v })),
}));

/** True when calls can be placed: every setup step is done. */
export function phoneReady(s: StatusView | null): boolean {
  return !!s && s.hasTwilio && s.hasElevenLabs && s.hasRelaySecrets && s.relayConnected && !!s.settings.agentId && !!s.settings.ownerVerified;
}
```

- [ ] **Step 4: Implement `PhoneSettings.tsx`**

Sections, top to bottom (each a `SettingsSection`):
1. **Setup** — a checklist of six rows with a check or empty circle icon: "Twilio account" (`hasTwilio`), "ElevenLabs key" (`hasElevenLabs`), "Relay secrets" (`hasRelaySecrets`), "Relay connected" (`relayConnected`), "Number connected" (`!!settings.agentId`), "Your number verified" (`settings.ownerVerified`). Shows `status.lastError` as an `ErrorLine` when set.
2. **Twilio** — `Input` "Account SID", `Input type="password"` "Auth token", button "Save Twilio" → `Jarvis.setSecrets({ twilioSid, twilioToken })`, then clear both inputs and `apply(result)`. `Input` "Jarvis number" (E.164) saved through Configure.
3. **ElevenLabs** — password `Input` "API key", button "Save key" → `setSecrets({ elevenLabsKey })`.
4. **Relay** — `Input` "Relay URL"; button "Generate relay secrets" → shows the three values in a monospace block with one copy button each (`aria-label="Copy relay token"` etc., icon-only with a `Tooltip` "Copy"), plus the four `wrangler` commands from `relay/README.md`. Warn: "Shown once. Generating again replaces them, and the relay must be updated." Button "Connect" → `Jarvis.configure({...settings, relayUrl, twilioNumber})`.
5. **Your phone** — `Input` "Your phone number", button "Call me with a code" → `startVerification(number)`; after it resolves, `Input` "Code" (inputMode numeric, maxLength 6) and button "Verify" → `confirmVerification(code)`. Disabled with a `Tooltip` explaining why until rows 1–5 of the checklist are done.
6. **Calls** — quiet hours start/end (`Input type="time"`), daily call cap and monthly cost cap (number fields), default voice and default voice model (`Select`, items from `Jarvis.voices()` / `Jarvis.models()`, model items labelled `"<name> · fast"` or `"<name> · expressive"`), button "Save" → `configure`. A line "Calls today: N · This month: $X.XX".
7. A footnote: "Calls need Orole running on this PC and the relay online. Phone conversations pass through ElevenLabs and your relay."

Use the `VoiceSettings.tsx` loading/error flow:

```tsx
export function PhoneSettings() {
  const status = useJarvis((s) => s.status);
  const error = useJarvis((s) => s.error);
  useEffect(() => {
    const stop = useJarvis.getState().startSync();
    void useJarvis.getState().load();
    return stop;
  }, []);
  if (!inDesktop) return <p role="status" className="rounded-lg border bg-card p-3 text-muted-foreground">Phone settings are available in the Orole OS desktop app.</p>;
  if (!status) return error ? <Failure error={error} retry={() => void useJarvis.getState().load()} /> : <Loading>Loading phone settings…</Loading>;
  return (
    <div className="space-y-4">
      {error && <Failure error={error} retry={() => void useJarvis.getState().load()} />}
      <SetupChecklist status={status} />
      <TwilioSection status={status} />
      <ElevenLabsSection />
      <RelaySection status={status} />
      <OwnerSection status={status} />
      <CallsSection status={status} />
    </div>
  );
}
```

Each sub-component keeps its own local input state, calls one `Jarvis` method, applies the returned `StatusView` with `useJarvis.getState().apply`, and shows its own error with `<p role="alert" className="text-xs text-destructive-foreground">`. Secrets are held only in local component state and cleared after saving.

- [ ] **Step 5: Register the section**

In `SettingsView.tsx` import `Phone` from lucide-react and `PhoneSettings`; add `{ id: "phone", label: "Phone", icon: Phone }` after the Voice entry and `{id === "phone" && <PhoneSettings />}` in the render block.

- [ ] **Step 6: Run checks**

Run: `bun run --cwd frontend check`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/jarvis/ frontend/src/features/settings/SettingsView.tsx
git commit -m "feat(frontend): add phone settings for jarvis"
```

---

### Task 13: Bot Jarvis page and call card

**Files:**
- Create: `frontend/src/features/jarvis/JarvisPage.tsx`, `frontend/src/features/jarvis/JarvisPage.test.tsx`, `frontend/src/features/jarvis/CallCard.tsx`, `frontend/src/features/jarvis/CallCard.test.tsx`
- Modify: `frontend/src/features/chat/builder/botForm.ts` (form fields), `frontend/src/features/chat/BotPanel.tsx` (page `"jarvis"`, NavRow in the "Does" group), `frontend/src/features/chat/parts/MessageParts.tsx` (`case "call"`), `frontend/src/features/chat/runtime/convert.ts` (`call: true`)

**Interfaces:**
- Consumes: `FormState`, `patch`, `Jarvis.rules/deleteRule/recentCalls/testCall/voices/models`, `useJarvis`, `phoneReady`, `toolAccessOf`/`permissionModeOf` from `features/chat/botTrust.ts`, row primitives from `features/chat/panel/rows.tsx`.
- Produces: `FormState.jarvisEnabled: boolean`, `jarvisPolicy: string`, `jarvisVoice: string`, `jarvisModel: string`; `JarvisPage({ bot, form, patch })`; `CallCard({ part })`.

- [ ] **Step 1: Extend the form model**

In `botForm.ts` add the four fields to `FormState` (defaults `false`, `""`, `""`, `""` in `initialForm`); in `formFromInput` read them from `config.jarvis`; in `toInput` write

```ts
...(form.jarvisEnabled || form.jarvisPolicy || form.jarvisVoice || form.jarvisModel
  ? { jarvis: { ...(form.jarvisEnabled ? { enabled: true } : {}), ...(form.jarvisPolicy ? { policy: form.jarvisPolicy } : {}),
      ...(form.jarvisVoice ? { voiceId: form.jarvisVoice } : {}), ...(form.jarvisModel ? { ttsModel: form.jarvisModel } : {}) } }
  : {}),
```

include them in `fingerprint`, and in `validate` reject a policy longer than 2000 characters with "Keep \"When to call me\" under 2000 characters." Extend the existing botForm tests with a round-trip case.

- [ ] **Step 2: Write the failing tests**

`JarvisPage.test.tsx`:

```tsx
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ rules: vi.fn(), deleteRule: vi.fn(), recentCalls: vi.fn(), testCall: vi.fn(), voices: vi.fn(), models: vi.fn(), all: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Jarvis: { rules: api.rules, deleteRule: api.deleteRule, recentCalls: api.recentCalls, testCall: api.testCall, voices: api.voices, models: api.models, status: vi.fn() },
  Settings: { all: api.all },
  events: { jarvisChanged: { on: () => () => undefined } },
}));

import { initialForm } from "@/features/chat/builder/botForm";
import { JarvisPage } from "./JarvisPage";
import { useJarvis } from "./store";

const ready = { settings: { agentId: "ag", ownerVerified: true }, hasTwilio: true, hasElevenLabs: true, hasRelaySecrets: true, relayConnected: true, callsToday: 0, monthCostMicros: 0 };
const bot = { id: "b1", name: "Atlas" } as never;
const full = { ...initialForm, codeTasks: true, toolAccess: "all", taskPermissionMode: "yolo" };

beforeEach(() => {
  vi.clearAllMocks();
  api.rules.mockResolvedValue([{ id: "r1", botId: "b1", trigger: "task_failed", target: "any", repeat: true, note: "" }]);
  api.recentCalls.mockResolvedValue([]);
  api.voices.mockResolvedValue([{ id: "v1", name: "Rachel" }]);
  api.models.mockResolvedValue([{ id: "eleven_flash_v2_5", name: "Flash v2.5", latency: "fast" }]);
  api.all.mockResolvedValue({ agents: { botYolo: true } });
  useJarvis.setState({ status: ready, error: null });
});
afterEach(cleanup);

it("sends users to phone setup when it is not done", async () => {
  useJarvis.setState({ status: { ...ready, relayConnected: false } });
  render(<JarvisPage bot={bot} form={full} patch={vi.fn()} />);
  expect(await screen.findByRole("button", { name: "Set up phone calls" })).toBeInTheDocument();
  expect(screen.queryByRole("switch", { name: "Jarvis mode" })).toBeNull();
});

it("disables the toggle and says why without full permissions", async () => {
  render(<JarvisPage bot={bot} form={{ ...full, toolAccess: "write" }} patch={vi.fn()} />);
  const toggle = await screen.findByRole("switch", { name: "Jarvis mode" });
  expect(toggle).toHaveAttribute("aria-disabled", "true");
  expect(screen.getByText(/tool access set to All/)).toBeInTheDocument();
});

it("turns Jarvis on and edits the policy", async () => {
  const patch = vi.fn();
  render(<JarvisPage bot={bot} form={full} patch={patch} />);
  fireEvent.click(await screen.findByRole("switch", { name: "Jarvis mode" }));
  expect(patch).toHaveBeenCalledWith({ jarvisEnabled: true });
  fireEvent.change(screen.getByLabelText("When to call me"), { target: { value: "Only failures" } });
  expect(patch).toHaveBeenCalledWith({ jarvisPolicy: "Only failures" });
});

it("lists and deletes rules", async () => {
  api.deleteRule.mockResolvedValue(undefined);
  render(<JarvisPage bot={bot} form={{ ...full, jarvisEnabled: true }} patch={vi.fn()} />);
  expect(await screen.findByText("When any task I start fails")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Delete rule: When any task I start fails" }));
  await waitFor(() => expect(api.deleteRule).toHaveBeenCalledWith("r1"));
});
```

`CallCard.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { CallCard } from "./CallCard";

it("renders a finished outbound call", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "outbound", status: "completed", title: "deploy failed", durationMs: 75_000, summary: "est. $0.15" }} />);
  expect(screen.getByText("Called you")).toBeInTheDocument();
  expect(screen.getByText("deploy failed")).toBeInTheDocument();
  expect(screen.getByText("1:15 · est. $0.15")).toBeInTheDocument();
});

it("renders a missed call", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "outbound", status: "voicemail", title: "deploy failed" }} />);
  expect(screen.getByText("Missed call")).toBeInTheDocument();
});

it("renders a call in progress", () => {
  render(<CallCard part={{ type: "call", id: "c1", name: "inbound", status: "in_progress", title: "You called" }} />);
  expect(screen.getByText("You called · on the line")).toBeInTheDocument();
});
```

- [ ] **Step 3: Run them and see them fail**

Run: `bun run --cwd frontend vitest run src/features/jarvis/`
Expected: FAIL (modules not found).

- [ ] **Step 4: Implement `CallCard.tsx`**

```tsx
import { PhoneIncomingIcon, PhoneMissedIcon, PhoneOutgoingIcon } from "lucide-react";
import type { MsgPart } from "@/mygo";

function clock(ms: number): string {
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export function CallCard({ part }: { part: MsgPart }) {
  const missed = part.status === "voicemail" || part.status === "no_answer" || part.status === "failed";
  const live = part.status === "dialing" || part.status === "in_progress";
  const inbound = part.name === "inbound";
  const Icon = missed ? PhoneMissedIcon : inbound ? PhoneIncomingIcon : PhoneOutgoingIcon;
  const label = missed ? "Missed call" : inbound ? (live ? "You called · on the line" : "You called") : live ? "Calling you…" : "Called you";
  const meta = [part.durationMs ? clock(part.durationMs) : "", part.summary ?? ""].filter(Boolean).join(" · ");
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2 text-sm">
      <Icon aria-hidden className={missed ? "size-4 text-destructive-foreground" : "size-4 text-muted-foreground"} />
      <div className="min-w-0">
        <p className="font-medium">{label}</p>
        {part.title && <p className="truncate text-muted-foreground">{part.title}</p>}
      </div>
      {meta && <p className="ml-auto shrink-0 text-xs text-muted-foreground">{meta}</p>}
    </div>
  );
}
```

In `MessageParts.tsx` add `case "call": blocks.push(<CallCard key={i} part={part} />); break;` and in `convert.ts` add `call: true` to `DATA_PARTS`. Add a case to `MessageParts.test.tsx` asserting a `call` part renders "Called you".

- [ ] **Step 5: Implement `JarvisPage.tsx`**

Layout using the `rows.tsx` primitives:
- If `!phoneReady(status)`: a short paragraph "Phone calls aren't set up yet." and a button "Set up phone calls" → `useUI.getState().openSettings("phone")`. Nothing else.
- Group "Jarvis": `SwitchRow` "Jarvis mode" (hint "Lets {bot.name} phone you and take your calls."). Compute the gate client-side for display only, from `form` plus `Settings.all()` agents (`botYolo`, `yoloDisabled`), mirroring `CheckGate` messages exactly; when it fails, the switch is disabled and the first failing reason is shown below it with a link button "Open permissions" that sets the panel page to `"permissions"`. The daemon re-checks on save (the save error surfaces through BotPanel's existing `Alert`).
- Group "When to call me": a `Textarea` labelled "When to call me" (placeholder "Only call for failures or when you need a decision. Never after 22:00."), counter `n/2000`.
- Group "Voice": `Select` "Voice" (first item "Default" = `""`) and `Select` "Voice model" (first item "Default", others `"<name> · fast|expressive"`).
- Group "Rules": rows from `Jarvis.rules(bot.id)`, each described in plain words by `describeRule(rule)`:
  - `task_finished`/`task_failed` with `any` → "When any task I start finishes/fails"; with an id → "When task <id first 8> finishes/fails"
  - `automation_*` → "When any automation / automation <id> succeeds/fails"
  - `notification` → "On <kind> notifications" (+ " from <source>")
  - `at_time` → "Every day at HH:MM" if repeat else "At HH:MM"
  - suffixes " · once" when not repeat, " · even in quiet hours", " · urgent", " · until <date>" when `expiresAt`.
  Each row has an icon-only delete button with `aria-label={"Delete rule: " + description}` and a `Tooltip` "Delete". Empty state: "No rules yet. Tell {bot.name} in chat, e.g. "call me when the deploy finishes"."
- Group "Recent calls": up to 5 from `Jarvis.recentCalls(bot.id)` as `CallCard`s, and a button "Test call" → `Jarvis.testCall(bot.id)` (disabled unless `form.jarvisEnabled`), showing the returned message or error.

Put `describeRule` in `features/jarvis/describe.ts` with its own small table test.

In `BotPanel.tsx`: add `"jarvis"` to `Page`, a `NavRow` with `PhoneIcon` labelled "Phone calls" (value "On"/"Off") in the "Does" group, and the page render line `{page === "jarvis" && <><PageHeader title="Phone calls" onBack={back} /><JarvisPage bot={bot} form={form} patch={patch} /></>}`.

- [ ] **Step 6: Run checks**

Run: `bun run --cwd frontend check`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/
git commit -m "feat(frontend): add bot phone calls page and call cards"
```

---

### Task 14: Docs, changelog, end-to-end checklist

**Files:**
- Create: `docs/architecture/jarvis.md`, `docs/development/jarvis-e2e.md`
- Modify: `docs/security/threat-model.md`, `docs/architecture/chat.md`, `docs/architecture/database-schema.md`, `docs/architecture/daemon.md`, `CHANGELOG.md`

- [ ] **Step 1: Write `docs/architecture/jarvis.md`**

Sections: overview (one paragraph + the architecture diagram from the spec §3), the permission gate (four conditions, where enforced), rules and triggers (table from spec §5.1), call lifecycle (outbound, inbound, voice turn, fillers, injections, switch_bot), speed design (spec §7), settings and secrets (table of credential keys from `types.go`), relay protocol (frame table from Task 4), failure behaviour (spec §9 table), and the explicit statement: "Calls need the daemon running and the relay reachable. Terminal sessions, tasks and automations behave as usual; a call does not survive a daemon restart."

- [ ] **Step 2: Update the threat model**

Add a "Jarvis phone calls" section: the relay as a new trust boundary (sees in-flight transcripts and turns, stores nothing; token rotation through "Generate relay secrets"), ElevenLabs as a processor of call audio and transcripts, full-permission bots reachable by phone, caller-ID spoofing as an accepted risk (owner decision 2026-10-14) with the mitigations (verified number only, a notification on every inbound call, audit entries, voicemail detection with nothing spoken), and prompt-injection handling (event details fenced).

- [ ] **Step 3: Update chat, schema and daemon docs**

- `chat.md`: voice turns (own run key, voice style, reasoning, limits, callbacks), `PartCall`, `Compose`, the save check hook.
- `database-schema.md`: `jarvis_rules`, `jarvis_calls`.
- `daemon.md`: the Jarvis service, its start/stop position, its IPC methods and the `jarvis.changed` event.

- [ ] **Step 4: Write `docs/development/jarvis-e2e.md`**

A manual checklist to run with real accounts (owner runs it; nothing in CI touches providers):
1. Deploy the relay (`relay/README.md`), paste secrets, Connect; checklist all green.
2. Verify your number with the code call.
3. Bot with full permissions → enable Jarvis; lower tool access → toggle disables with the reason.
4. In chat: "call me when task X finishes" → rule listed; finish the task → phone rings within 10 s; the first sentence contains the bot's name and not the app name.
5. On the call: "what's running?" (answers with a filler then a summary), "start a coding task to …" (task appears in the app during the call), "leave it" (bot acknowledges and ends).
6. Decline the call → notification "<bot> tried to call".
7. Let voicemail answer → nothing about the task is said; notification arrives.
8. Call the Jarvis number from your phone → executive bot greets by name; "put me through to <other bot>" → that bot introduces itself.
9. Call from another number → rejected, audit entry `jarvis.inbound.rejected`.
10. Quiet hours: set them around now, fire a rule → notification instead of a call; a rule with "even in quiet hours" → call.
11. Stop the daemon mid-call → the relay says "Lost connection, I'll message you." within ~5 s.
12. Record per-turn latency from the call record; target first audio ≤ 300 ms after pickup, reply start ≤ 1.2 s.

- [ ] **Step 5: Changelog**

Under the unreleased section: "Jarvis mode: bots with full permissions can phone you when you tell them to (rules from chat) or when their \"When to call me\" policy says so, and you can call them. Needs a Twilio number, an ElevenLabs key and a small Cloudflare relay (Settings › Phone)."

- [ ] **Step 6: Run every check**

Run: `go fmt ./... && go vet ./... && go test -race ./... && bun run --cwd frontend check && bun run --cwd relay test`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add docs/ CHANGELOG.md
git commit -m "docs(jarvis): document phone calls, relay and threat model"
```

---

### Task 15: Deploy the relay (owner approval required)

This publishes a Worker to the owner's Cloudflare account. **Ask the owner in chat and wait for an explicit yes before Step 2.**

- [ ] **Step 1: Confirm**

Ask: "Ready to deploy the relay as `orole-jarvis-relay` to your Cloudflare account (ray.orole@gmail.com)? It creates one Worker with one Durable Object on the free plan." Wait for yes.

- [ ] **Step 2: Deploy**

Run (from `relay/`): `bunx wrangler@4 deploy`
Expected: a `https://orole-jarvis-relay.<subdomain>.workers.dev` URL.

- [ ] **Step 3: Hand over the secrets step**

The three secrets are generated in the app and must be entered by the owner: tell them to open Settings › Phone → Relay → "Generate relay secrets", then run `bunx wrangler@4 secret put RELAY_TOKEN` (and `LLM_SECRET`, `WEBHOOK_SECRET`) in `relay/` and paste each value, then paste the Worker URL into "Relay URL" and press Connect. Do not handle the secret values yourself.

- [ ] **Step 4: Check health**

Run: `curl -s https://orole-jarvis-relay.<subdomain>.workers.dev/health`
Expected: `{"daemon":true}` once the app is connected.
