// Package memory implements the chat memory subsystem on top of migration 0097:
//
//   - core memory: small curated notes per scope (bot:<id>, user, shared:<name>) that go into every
//     prompt as a snapshot (Hermes-style bounded notes, edited through the `memory` tool);
//   - archival facts: searchable facts extracted by idle consolidation, found with memory_search and
//     recalled automatically for the user's text;
//   - history search over the bot's own conversation, knowledge files, ask_bot delegation and skill
//     proposals.
//
// Access is enforced here, in Go, on every path: a bot always has read+write on bot:<its id>; any
// other scope needs a grant in bot.Config.Memory, which is the source of truth (bot_memory_access
// rows are synced from it before every decision, so no stale row can widen access). Queries filter
// by the allowed scope set inside SQL, never after the fact.
//
// Retrieval is isolated behind Retriever so a vector index can be added later without schema
// changes: an embedding retriever would keep its vectors in a sidecar table keyed by
// memory_entries.id / knowledge_chunks.id, return Hits for the same scope sets, and be merged with
// the FTS hits by reciprocal-rank fusion in Store. Nothing here depends on embeddings.
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// Config wires a Store.
type Config struct {
	DB      *sql.DB
	DataDir string
	// LLM resolves the memory model for consolidation; nil disables consolidation.
	LLM llm.Service
	// Skills is the central registry proposals are saved into on approval; nil disables propose_skill.
	Skills *skills.Store
	// Grants gives a bot an approved skill; nil leaves the skill in the registry only.
	Grants SkillGranter
	// Delegator runs ask_bot; it may be attached later with SetDelegator. Without one ask_bot is not offered.
	Delegator chat.Delegator
	// Publish delivers the chat.memory event. Must not block.
	Publish func(event string, payload any)
	// Audit records security-relevant decisions (skill approval, scope deletion).
	Audit func(action, target, outcome, detail string)
	Log   *slog.Logger
	Now   func() time.Time
}

// Store is the memory subsystem. It implements chat.Memory.
type Store struct {
	cfg Config
	db  *sql.DB
	log *slog.Logger
	now func() time.Time

	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	delegator chat.Delegator
	retriever Retriever
	running   map[string]struct{} // bots being consolidated
	failures  map[string]failure  // consecutive malformed consolidation answers per bot

	skillMu sync.Mutex // serialises proposal resolution
}

var _ chat.Memory = (*Store)(nil)

// New builds a Store. DB is required.
func New(cfg Config) (*Store, error) {
	if cfg.DB == nil {
		return nil, errors.New("memory: database is required")
	}
	s := &Store{cfg: cfg, db: cfg.DB, log: cfg.Log, now: cfg.Now, delegator: cfg.Delegator,
		running: map[string]struct{}{}, failures: map[string]failure{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.base, s.cancel = context.WithCancel(context.Background())
	s.retriever = ftsRetriever{db: cfg.DB}
	return s, nil
}

// Close cancels running consolidations and waits for them.
func (s *Store) Close() {
	s.cancel()
	s.wg.Wait()
}

// SetDelegator attaches the engine that runs ask_bot.
func (s *Store) SetDelegator(d chat.Delegator) {
	s.mu.Lock()
	s.delegator = d
	s.mu.Unlock()
}

// SetRetriever replaces the retrieval backend (see the package comment).
func (s *Store) SetRetriever(r Retriever) {
	s.mu.Lock()
	s.retriever = r
	s.mu.Unlock()
}

func (s *Store) getDelegator() chat.Delegator {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delegator
}

func (s *Store) getRetriever() Retriever {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retriever
}

func (s *Store) nowMs() int64 { return s.now().UnixMilli() }

func (s *Store) publish(scope string) {
	if s.cfg.Publish != nil && scope != "" {
		s.cfg.Publish("chat.memory", chat.MemoryChange{Scope: scope})
	}
}

func (s *Store) audit(action, target, outcome, detail string) {
	if s.cfg.Audit != nil {
		s.cfg.Audit(action, target, outcome, detail)
	}
}

// ---- errors ----

type userError struct {
	kind error
	msg  string
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.kind }

func invalidf(format string, a ...any) error {
	return &userError{kind: chat.ErrInvalid, msg: fmt.Sprintf(format, a...)}
}

func notFoundf(format string, a ...any) error {
	return &userError{kind: chat.ErrNotFound, msg: fmt.Sprintf(format, a...)}
}

// ---- database helpers ----

type querier interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// writeTx starts a transaction that holds SQLite's write lock from the first statement. A deferred
// transaction that reads first and writes later can fail with SQLITE_BUSY_SNAPSHOT when another
// writer committed in between; taking the lock up front makes the busy timeout apply instead.
func (s *Store) writeTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memory_scopes SET name = name WHERE 0`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func anySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, v := range ss {
		out[i] = v
	}
	return out
}

func newID() string { return uuid.NewString() }

// ---- bots and access ----

// loadBot reads the identity and config of an active bot straight from the database, so tools always
// decide with the current delegation list and grants.
func (s *Store) loadBot(ctx context.Context, q querier, id string) (chat.Bot, error) {
	var b chat.Bot
	var cfg string
	err := q.QueryRowContext(ctx,
		`SELECT id, name, description, provider, model, config FROM bots WHERE id = ? AND archived_at IS NULL`, id).
		Scan(&b.ID, &b.Name, &b.Description, &b.Provider, &b.Model, &cfg)
	if errors.Is(err, sql.ErrNoRows) {
		return chat.Bot{}, notFoundf("bot %q not found", id)
	}
	if err != nil {
		return chat.Bot{}, err
	}
	if cfg != "" {
		if err := json.Unmarshal([]byte(cfg), &b.Config); err != nil {
			return chat.Bot{}, fmt.Errorf("bot %s: bad config: %w", id, err)
		}
	}
	return b, nil
}

type grant struct{ Read, Write bool }

func ownScope(botID string) string { return chat.BotPrefix + botID }

// validGrantScope reports whether a scope id may appear in a grant: only the shared user profile and
// shared:<name> scopes. A bot:<id> scope is never grantable to another bot.
func validGrantScope(scope string) bool {
	return scope == chat.ScopeUser || (strings.HasPrefix(scope, chat.SharedPrefix) && len(scope) > len(chat.SharedPrefix))
}

// grants returns the scopes bot may use besides its own, after syncing bot_memory_access with
// bot.Config.Memory. Write implies read. Grants to scopes that no longer exist are dropped.
func (s *Store) grants(ctx context.Context, bot chat.Bot) (map[string]grant, error) {
	if bot.ID == "" {
		return nil, errors.New("memory: bot has no id")
	}
	want := map[string]grant{}
	for _, a := range bot.Config.Memory {
		if !validGrantScope(a.Scope) {
			continue
		}
		g := want[a.Scope]
		g.Write = g.Write || a.Write
		g.Read = g.Read || a.Read || a.Write
		want[a.Scope] = g
	}
	if len(want) > 0 {
		exist, err := s.existingScopes(ctx, s.db)
		if err != nil {
			return nil, err
		}
		for sc := range want {
			if !exist[sc] {
				delete(want, sc)
			}
		}
	}
	have, err := s.accessRows(ctx, s.db, bot.ID)
	if err != nil {
		return nil, err
	}
	if !grantsEqual(have, want) {
		if err := s.replaceAccess(ctx, bot.ID, want); err != nil {
			return nil, err
		}
	}
	return want, nil
}

func grantsEqual(a, b map[string]grant) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (s *Store) existingScopes(ctx context.Context, q querier) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM memory_scopes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Store) accessRows(ctx context.Context, q querier, botID string) (map[string]grant, error) {
	rows, err := q.QueryContext(ctx, `SELECT scope, can_read, can_write FROM bot_memory_access WHERE bot_id = ?`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]grant{}
	for rows.Next() {
		var sc string
		var r, w int
		if err := rows.Scan(&sc, &r, &w); err != nil {
			return nil, err
		}
		out[sc] = grant{Read: r == 1, Write: w == 1}
	}
	return out, rows.Err()
}

func (s *Store) replaceAccess(ctx context.Context, botID string, want map[string]grant) error {
	tx, err := s.writeTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_memory_access WHERE bot_id = ?`, botID); err != nil {
		return err
	}
	for sc, g := range want {
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_memory_access(bot_id, scope, can_read, can_write) VALUES (?,?,?,?)`,
			botID, sc, b2i(g.Read), b2i(g.Write)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// access is the resolved scope set of one bot.
type access struct {
	botID  string
	grants map[string]grant
}

func (s *Store) accessFor(ctx context.Context, bot chat.Bot) (access, error) {
	g, err := s.grants(ctx, bot)
	if err != nil {
		return access{}, err
	}
	return access{botID: bot.ID, grants: g}, nil
}

func (a access) canRead(scope string) bool {
	if scope == ownScope(a.botID) {
		return true
	}
	return a.grants[scope].Read
}

func (a access) canWrite(scope string) bool {
	if scope == ownScope(a.botID) {
		return true
	}
	return a.grants[scope].Write
}

// readable lists readable scopes: own first, then grants sorted.
func (a access) readable() []string {
	out := []string{ownScope(a.botID)}
	for _, sc := range slices.Sorted(maps.Keys(a.grants)) {
		if a.grants[sc].Read {
			out = append(out, sc)
		}
	}
	return out
}

func (a access) writable() []string {
	out := []string{ownScope(a.botID)}
	for _, sc := range slices.Sorted(maps.Keys(a.grants)) {
		if a.grants[sc].Write {
			out = append(out, sc)
		}
	}
	return out
}

// ---- scopes ----

func scopeLimit(scope string) int {
	switch {
	case scope == chat.ScopeUser:
		return chat.CoreUserLimit
	case strings.HasPrefix(scope, chat.SharedPrefix):
		return chat.CoreSharedLimit
	default:
		return chat.CoreBotLimit
	}
}

func scopeKind(scope string) string {
	switch {
	case scope == chat.ScopeUser:
		return "user"
	case strings.HasPrefix(scope, chat.SharedPrefix):
		return "shared"
	default:
		return "bot"
	}
}

// scopeName returns the display name of a scope.
func (s *Store) scopeName(ctx context.Context, q querier, scope string) (string, error) {
	switch scopeKind(scope) {
	case "bot":
		var name string
		err := q.QueryRowContext(ctx, `SELECT name FROM bots WHERE id = ?`, strings.TrimPrefix(scope, chat.BotPrefix)).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return "", notFoundf("scope %q not found", scope)
		}
		return name, err
	default:
		var name string
		err := q.QueryRowContext(ctx, `SELECT name FROM memory_scopes WHERE id = ?`, scope).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return "", notFoundf("scope %q not found", scope)
		}
		return name, err
	}
}

// ---- entries ----

const entryCols = `id, scope, kind, text, trust, author, source_msg_id, pending, created_at, updated_at, COALESCE(invalidated_at, 0)`

type scanner interface{ Scan(dest ...any) error }

func scanEntry(sc scanner) (chat.MemoryEntry, error) {
	var e chat.MemoryEntry
	var pending int
	err := sc.Scan(&e.ID, &e.Scope, &e.Kind, &e.Text, &e.Trust, &e.Author, &e.SourceMsgID, &pending, &e.CreatedAt, &e.UpdatedAt, &e.InvalidatedAt)
	e.Pending = pending == 1
	return e, err
}

func queryEntries(ctx context.Context, q querier, where string, args ...any) ([]chat.MemoryEntry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+entryCols+` FROM memory_entries `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chat.MemoryEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func getEntry(ctx context.Context, q querier, id string) (chat.MemoryEntry, error) {
	e, err := scanEntry(q.QueryRowContext(ctx, `SELECT `+entryCols+` FROM memory_entries WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return chat.MemoryEntry{}, notFoundf("memory entry %q not found", id)
	}
	return e, err
}

// supersedesPrefix marks a pending entry that replaces an active one once approved. The marker lives
// in invalidated_by while invalidated_at stays NULL, so the entry is still "not invalidated".
const supersedesPrefix = "supersedes:"

func insertEntry(ctx context.Context, q querier, e chat.MemoryEntry, invalidatedBy string) (chat.MemoryEntry, error) {
	if e.ID == "" {
		e.ID = newID()
	}
	_, err := q.ExecContext(ctx, `INSERT INTO memory_entries(id, scope, kind, text, trust, author, source_msg_id, pending, created_at, updated_at, invalidated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Scope, e.Kind, e.Text, e.Trust, e.Author, e.SourceMsgID, b2i(e.Pending), e.CreatedAt, e.UpdatedAt, invalidatedBy)
	return e, err
}

func invalidateEntry(ctx context.Context, q querier, id, by string, at int64) error {
	_, err := q.ExecContext(ctx, `UPDATE memory_entries SET invalidated_at = ?, invalidated_by = ?, updated_at = ? WHERE id = ? AND invalidated_at IS NULL`, at, by, at, id)
	return err
}

// coreRow is a core entry plus the id it will replace when approved.
type coreRow struct {
	chat.MemoryEntry
	supersedes string
}

// effectiveCore lists the core entries of a scope as they will be once pending writes are approved:
// active entries, minus those a pending entry replaces, plus the pending entries. Oldest first.
func effectiveCore(ctx context.Context, q querier, scope string) ([]coreRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+entryCols+`, invalidated_by FROM memory_entries
		WHERE scope = ? AND kind = 'core' AND invalidated_at IS NULL ORDER BY created_at, rowid`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []coreRow
	superseded := map[string]bool{}
	for rows.Next() {
		var e chat.MemoryEntry
		var pending int
		var by string
		if err := rows.Scan(&e.ID, &e.Scope, &e.Kind, &e.Text, &e.Trust, &e.Author, &e.SourceMsgID, &pending, &e.CreatedAt, &e.UpdatedAt, &e.InvalidatedAt, &by); err != nil {
			return nil, err
		}
		e.Pending = pending == 1
		r := coreRow{MemoryEntry: e}
		if e.Pending && strings.HasPrefix(by, supersedesPrefix) {
			r.supersedes = strings.TrimPrefix(by, supersedesPrefix)
			superseded[r.supersedes] = true
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := all[:0]
	for _, r := range all {
		if !superseded[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

// coreChars is the budget used by entries joined with the entry separator.
func coreChars(texts []string) int {
	n := 0
	for _, t := range texts {
		n += len([]rune(t))
	}
	if len(texts) > 1 {
		n += (len(texts) - 1) * len([]rune(entrySep))
	}
	return n
}

func coreTexts(rows []coreRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Text
	}
	return out
}

// entrySep separates core entries in the prompt and in the character budget (Hermes delimiter).
const entrySep = "\n§\n"

// commas formats n with thousands separators.
func commas(n int) string {
	s := fmt.Sprint(n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func usageLabel(used, limit int) string {
	pct := 0
	if limit > 0 {
		pct = used * 100 / limit
	}
	return fmt.Sprintf("[%d%% - %s/%s chars]", pct, commas(used), commas(limit))
}

// runeTrunc shortens s to at most n runes, adding an ellipsis when cut.
func runeTrunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos] + "…"
		}
		i++
	}
	return s
}

// oneLine collapses all whitespace into single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
