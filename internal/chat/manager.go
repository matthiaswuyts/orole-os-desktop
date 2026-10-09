package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Sentinel errors. Handlers map them to IPC codes; messages are safe to show to the user.
var (
	ErrInvalid  = errors.New("invalid request")
	ErrNotFound = errors.New("not found")
	ErrBusy     = errors.New("busy")
	ErrClosed   = errors.New("chat is shutting down")
)

// userError carries a user-facing message and the sentinel it maps to.
type userError struct {
	kind error
	msg  string
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.kind }

func invalidf(format string, args ...any) error {
	return &userError{kind: ErrInvalid, msg: fmt.Sprintf(format, args...)}
}

func notFoundf(format string, args ...any) error {
	return &userError{kind: ErrNotFound, msg: fmt.Sprintf(format, args...)}
}

func busyf(format string, args ...any) error {
	return &userError{kind: ErrBusy, msg: fmt.Sprintf(format, args...)}
}

const (
	maxQueued          = 20 // queued user messages per bot
	maxDelegationDepth = 2
	defaultIdleAfter   = 2 * time.Minute
	catalogTTL         = 5 * time.Minute
)

// Config wires the Manager.
type Config struct {
	DB      *sql.DB
	DataDir string
	LLM     llm.Service
	// Publish delivers chat.message, chat.run and chat.bot events. It must not block and must not
	// call back into the Manager.
	Publish func(event string, payload any)
	Log     *slog.Logger
	Clock   func() time.Time
	// IdleConsolidateAfter is how long a bot must be idle before Memory.Consolidate runs (default 2 minutes).
	IdleConsolidateAfter time.Duration
}

// Manager owns bots, conversations, runs, attachments and the tool registry. Policy decisions for
// individual tool calls live in the tools themselves (they receive the actor "bot:<id>").
type Manager struct {
	cfg   Config
	log   *slog.Logger
	store *store
	up    *uploads

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	memory   Memory
	tools    []Tool
	sections []func(Bot) string
	runs     map[string]*runState // active runs by bot id or "draft:<id>"
	idle     map[string]*idleJob
	nonces   nonceCache
	closed   bool

	catMu   sync.Mutex
	catalog map[string]catalogEntry

	closeOnce sync.Once
}

type catalogEntry struct {
	at     time.Time
	models []llm.Model
}

// New opens the chat service: it marks runs interrupted by a previous shutdown as incomplete,
// clears orphaned upload staging and starts the attachment expiry sweeper.
func New(cfg Config) (*Manager, error) {
	if cfg.DB == nil {
		return nil, errors.New("chat: database is required")
	}
	if cfg.DataDir == "" {
		return nil, errors.New("chat: data directory is required")
	}
	if cfg.LLM == nil {
		return nil, errors.New("chat: llm service is required")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.IdleConsolidateAfter <= 0 {
		cfg.IdleConsolidateAfter = defaultIdleAfter
	}
	m := &Manager{
		cfg:     cfg,
		log:     cfg.Log,
		store:   &store{db: cfg.DB, now: cfg.Clock},
		runs:    map[string]*runState{},
		idle:    map[string]*idleJob{},
		catalog: map[string]catalogEntry{},
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	up, err := newUploads(cfg.DataDir, m.store, cfg.Clock)
	if err != nil {
		m.cancel()
		return nil, err
	}
	m.up = up
	if err := m.store.markInterrupted(context.Background()); err != nil {
		m.cancel()
		return nil, err
	}
	m.wg.Add(1)
	go m.sweepLoop()
	return m, nil
}

// SetMemory attaches the memory subsystem. A nil Memory is valid: the engine then runs without it.
func (m *Manager) SetMemory(mem Memory) {
	m.mu.Lock()
	m.memory = mem
	m.mu.Unlock()
}

// AddTools registers tools offered to bots (each tool decides per bot and model whether it is available).
func (m *Manager) AddTools(tools ...Tool) {
	m.mu.Lock()
	m.tools = append(m.tools, tools...)
	m.mu.Unlock()
}

// AddPromptSection registers a function whose non-empty result is appended, as plain text, to every
// bot's system prompt after the standing rules. Sections appear in registration order. The text must
// be trusted (for example the user-approved skill index): it is not fenced.
func (m *Manager) AddPromptSection(fn func(bot Bot) string) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	m.sections = append(m.sections, fn)
	m.mu.Unlock()
}

func (m *Manager) promptSections(bot Bot) []string {
	m.mu.Lock()
	fns := slices.Clone(m.sections)
	m.mu.Unlock()
	var out []string
	for _, fn := range fns {
		if s := strings.TrimSpace(fn(bot)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Tools returns every registered tool, including the memory tools.
func (m *Manager) Tools() []Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.toolsLocked(false)
}

// toolsLocked lists registered tools. Drafts exclude the memory tools: they write persistent state
// and a draft has no bot identity.
func (m *Manager) toolsLocked(draft bool) []Tool {
	out := slices.Clone(m.tools)
	if m.memory != nil && !draft {
		out = append(out, m.memory.Tools()...)
	}
	return out
}

func (m *Manager) memoryOrNil() Memory {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.memory
}

func (m *Manager) emit(event string, payload any) {
	if m.cfg.Publish != nil {
		m.cfg.Publish(event, payload)
	}
}

func (m *Manager) publishMessage(msg Message) { m.emit("chat.message", msg) }

func (m *Manager) publishBot(id, action string) {
	m.emit("chat.bot", BotChange{ID: id, Action: action})
}

// Close cancels every run, waits for them to stop and removes staged uploads. Runs that were
// interrupted by shutdown are stored as incomplete. It is safe to call more than once.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		for _, j := range m.idle {
			j.timer.Stop()
		}
		clear(m.idle)
		m.mu.Unlock()
		m.cancel()
		m.wg.Wait()
		m.up.close()
	})
}

// Bots lists active bots, most recently active first.
func (m *Manager) Bots(ctx context.Context) ([]Bot, error) {
	bots, err := m.store.listBots(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	for i := range bots {
		_, bots[i].Running = m.runs[bots[i].ID]
	}
	m.mu.Unlock()
	return bots, nil
}

// Bot returns one active bot.
func (m *Manager) Bot(ctx context.Context, id string) (Bot, error) {
	b, err := m.store.getBot(ctx, id, false)
	if err != nil {
		return Bot{}, err
	}
	m.mu.Lock()
	_, b.Running = m.runs[id]
	m.mu.Unlock()
	return b, nil
}

// SaveBot creates (empty ID) or replaces a bot.
func (m *Manager) SaveBot(ctx context.Context, in BotInput) (Bot, error) {
	creating := in.ID == ""
	b, err := m.store.saveBot(ctx, in)
	if err != nil {
		return Bot{}, err
	}
	m.publishBot(b.ID, "saved")
	if creating && b.Config.Setup {
		if err := m.greet(ctx, b.ID); err != nil {
			m.log.Warn("chat: cannot start the setup conversation", "bot", b.ID, "err", err)
		}
	}
	return m.Bot(ctx, b.ID)
}

// setupKickoff is the hidden first message of a new bot: the model reads it, the user never sees it.
const setupKickoff = "(The user just created you. Start the setup now: greet them and ask your first question.)"

// greet makes a new bot speak first: it stores a hidden user message and runs a turn on it.
func (m *Manager) greet(ctx context.Context, botID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.runs[botID] != nil {
		return nil
	}
	msg := Message{
		ID: newID(), BotID: botID, Role: "user", Author: "user", Status: StatusComplete, CreatedAt: m.nowMs(),
		Parts: []MsgPart{{Type: PartText, Text: setupKickoff, Hidden: true}},
	}
	if err := m.store.insertMessage(ctx, &msg); err != nil {
		return err
	}
	m.publishMessage(msg)
	m.startLocked(botID, turn{userMsgID: msg.ID})
	return nil
}

// ArchiveBot stops the bot's run, then hides it. The conversation and memory are kept.
func (m *Manager) ArchiveBot(ctx context.Context, id string) error {
	err := m.whenIdle(ctx, id, func() error { return m.store.archiveBot(ctx, id) })
	if err == nil {
		m.cancelIdleTimer(id)
		m.publishBot(id, "archived")
	}
	return err
}

// ExportBot returns the portable form of a bot: settings and instructions, never memory, messages
// or secrets. Grants that name local resources (delegates, memory scopes, MCP tools, skills) are
// left out: an imported file must not be able to grant itself access.
func (m *Manager) ExportBot(ctx context.Context, id string) (BotExport, error) {
	b, err := m.store.getBot(ctx, id, false)
	if err != nil {
		return BotExport{}, err
	}
	return BotExport{Version: 1, Bot: portableInput(BotInput{
		Name: b.Name, Avatar: b.Avatar, Color: b.Color, Description: b.Description,
		Instructions: b.Instructions, Provider: b.Provider, Model: b.Model, Reasoning: b.Reasoning, Config: b.Config,
	})}, nil
}

// ImportBot creates a new bot from an export. The ID in the file is ignored.
func (m *Manager) ImportBot(ctx context.Context, in BotExport) (Bot, error) {
	if in.Version != 1 {
		return Bot{}, invalidf("Unsupported bot file version %d.", in.Version)
	}
	bi := portableInput(in.Bot)
	bi.ID = ""
	return m.SaveBot(ctx, bi)
}

func portableInput(in BotInput) BotInput {
	in.ID = ""
	in.Config.MCPTools = nil
	in.Config.Skills = nil
	in.Config.Executive = false
	in.Config.Memory = nil
	return in
}

// Messages pages a conversation (ascending order, newest page first).
func (m *Manager) Messages(ctx context.Context, req MessagesRequest) (MessagePage, error) {
	if _, err := m.store.getBot(ctx, req.BotID, false); err != nil {
		return MessagePage{}, err
	}
	return m.store.pageMessages(ctx, req)
}

// Clear stops the bot's run and deletes its conversation, summary and sent attachments. Memory is kept.
func (m *Manager) Clear(ctx context.Context, id string) error {
	err := m.whenIdle(ctx, id, func() error {
		shas, err := m.store.clearConversation(ctx, id)
		if err != nil {
			return err
		}
		m.up.gcBlobs(shas)
		return nil
	})
	if err == nil {
		m.cancelIdleTimer(id)
		m.publishBot(id, "cleared")
	}
	return err
}

// ModelCaps resolves provider and model (empty values use the defaults) and returns the model's
// capabilities from the cached provider catalogue.
func (m *Manager) ModelCaps(ctx context.Context, provider, model string) (llm.Caps, error) {
	_, mdl, err := m.modelInfo(ctx, provider, model)
	if err != nil {
		return llm.Caps{}, err
	}
	return mdl.Caps, nil
}

// resolveModel applies defaults to an incomplete provider/model pair.
func (m *Manager) resolveModel(ctx context.Context, provider, model string) (string, string, error) {
	if provider != "" && model != "" {
		return provider, model, nil
	}
	d, err := m.cfg.LLM.Defaults(ctx)
	if err != nil {
		return "", "", err
	}
	dp, dm, ok := llm.SplitModel(d.ChatModel)
	switch {
	case provider == "" && model == "":
		if !ok {
			return "", "", errNoModel
		}
		return dp, dm, nil
	case provider != "" && ok && dp == provider:
		return provider, dm, nil
	case provider == "":
		return "", "", errNoModel
	}
	return "", "", errNoModel
}

var errNoModel = &userError{kind: ErrInvalid, msg: "No model is chosen for this bot and no default model is set. Pick a model in the bot settings or set a default in Settings > Models."}

func (m *Manager) modelInfo(ctx context.Context, provider, model string) (string, llm.Model, error) {
	provider, model, err := m.resolveModel(ctx, provider, model)
	if err != nil {
		return "", llm.Model{}, err
	}
	models, err := m.catalogue(ctx, provider)
	if err != nil {
		return "", llm.Model{}, err
	}
	for _, mdl := range models {
		if mdl.ID == model {
			return provider, mdl, nil
		}
	}
	return "", llm.Model{}, invalidf("Model %q is not offered by %s. Pick another model in the bot settings.", model, provider)
}

// catalogue returns the provider's models, cached for catalogTTL.
func (m *Manager) catalogue(ctx context.Context, provider string) ([]llm.Model, error) {
	m.catMu.Lock()
	if e, ok := m.catalog[provider]; ok && m.cfg.Clock().Sub(e.at) < catalogTTL {
		m.catMu.Unlock()
		return e.models, nil
	}
	m.catMu.Unlock()
	models, err := m.cfg.LLM.Models(ctx, provider)
	if err != nil {
		return nil, err
	}
	m.catMu.Lock()
	m.catalog[provider] = catalogEntry{at: m.cfg.Clock(), models: models}
	m.catMu.Unlock()
	return models, nil
}

// nonceCache remembers recent client nonces so a retried send returns the stored message.
type nonceCache struct {
	ids  map[string]string
	ring [1024]string
	pos  int
}

func (c *nonceCache) get(botID, nonce string) (string, bool) {
	id, ok := c.ids[botID+"\x00"+nonce]
	return id, ok
}

func (c *nonceCache) put(botID, nonce, msgID string) {
	if c.ids == nil {
		c.ids = make(map[string]string, len(c.ring))
	}
	if old := c.ring[c.pos]; old != "" {
		delete(c.ids, old)
	}
	k := botID + "\x00" + nonce
	c.ring[c.pos] = k
	c.pos = (c.pos + 1) % len(c.ring)
	c.ids[k] = msgID
}

func newID() string { return uuid.NewString() }

func (m *Manager) nowMs() int64 { return m.cfg.Clock().UnixMilli() }

func (m *Manager) sweepLoop() {
	defer m.wg.Done()
	m.sweep()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.sweep()
		}
	}
}

func (m *Manager) sweep() {
	if err := m.up.sweep(m.ctx); err != nil && m.ctx.Err() == nil {
		m.log.Warn("chat attachment sweep failed", "err", err)
	}
}
