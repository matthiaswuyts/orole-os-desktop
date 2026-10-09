package memory

import (
	"context"
	"errors"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/chat"
)

// Tools returns the memory-related client tools. Each Tool's Specs decide per bot whether it is
// offered: ask_bot needs delegation targets and an engine, knowledge_search needs ready knowledge
// files, propose_skill needs the skills registry.
func (s *Store) Tools() []chat.Tool {
	return []chat.Tool{
		memoryTool{s}, memorySearchTool{s}, searchHistoryTool{s}, knowledgeTool{s}, askTool{s}, proposeSkillTool{s},
	}
}

// ---- IPC-facing operations (user actions: no bot access check, the user owns all memory) ----

const (
	maxSharedScopes  = 50
	maxScopeSlug     = 40
	maxScopeNameLen  = 60
	maxEntryChars    = 4000
	defaultListLimit = 100
	maxListLimit     = 500
)

// scopeStats fills Count/Used/Limit of a scope.
func (s *Store) scopeStats(ctx context.Context, sc *chat.MemoryScope) error {
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_entries WHERE scope = ? AND invalidated_at IS NULL AND pending = 0`, sc.ID).Scan(&sc.Count); err != nil {
		return err
	}
	rows, err := effectiveCore(ctx, s.db, sc.ID)
	if err != nil {
		return err
	}
	var texts []string
	for _, r := range rows {
		if !r.Pending {
			texts = append(texts, r.Text)
		}
	}
	sc.Used = coreChars(texts)
	sc.Limit = scopeLimit(sc.ID)
	return nil
}

// Scopes lists the visible scopes. With a bot id: that bot's private scope, the user profile and
// the shared scopes, with the bot's access filled in. Without: the private scopes of all active bots too.
func (s *Store) Scopes(ctx context.Context, botID string) ([]chat.MemoryScope, error) {
	var bot chat.Bot
	var g map[string]grant
	if botID != "" {
		var err error
		if bot, err = s.loadBot(ctx, s.db, botID); err != nil {
			return nil, err
		}
		if g, err = s.grants(ctx, bot); err != nil {
			return nil, err
		}
	}
	var out []chat.MemoryScope
	if botID != "" {
		out = append(out, chat.MemoryScope{ID: ownScope(botID), Name: bot.Name, Kind: "bot", Access: &chat.ScopeAccess{Scope: ownScope(botID), Read: true, Write: true}})
	} else {
		rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM bots WHERE archived_at IS NULL ORDER BY name, id`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, chat.MemoryScope{ID: ownScope(id), Name: name, Kind: "bot"})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM memory_scopes ORDER BY id != 'user', name, id`)
	if err != nil {
		return nil, err
	}
	var shared []chat.MemoryScope
	for rows.Next() {
		var sc chat.MemoryScope
		if err := rows.Scan(&sc.ID, &sc.Name); err != nil {
			rows.Close()
			return nil, err
		}
		sc.Kind = scopeKind(sc.ID)
		shared = append(shared, sc)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out = append(out, shared...)
	for i := range out {
		if err := s.scopeStats(ctx, &out[i]); err != nil {
			return nil, err
		}
		if botID != "" && out[i].Kind != "bot" {
			if gr, ok := g[out[i].ID]; ok {
				out[i].Access = &chat.ScopeAccess{Scope: out[i].ID, Read: gr.Read, Write: gr.Write}
			}
		}
	}
	return out, nil
}

// scopeSlug lowercases a display name into a scope slug.
func scopeSlug(name string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= maxScopeSlug {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// CreateScope creates a shared scope "shared:<slug>" from a display name.
func (s *Store) CreateScope(ctx context.Context, name string) (chat.MemoryScope, error) {
	name = oneLine(name)
	slug := scopeSlug(name)
	switch {
	case name == "" || slug == "":
		return chat.MemoryScope{}, invalidf("a scope needs a name with letters or digits")
	case len([]rune(name)) > maxScopeNameLen:
		return chat.MemoryScope{}, invalidf("scope names are limited to %d characters", maxScopeNameLen)
	case slug == chat.ScopeUser:
		// "shared:user" is fine technically, but is confusing next to the profile scope.
		return chat.MemoryScope{}, invalidf("choose a different name")
	}
	if err := checkUnsafe(name, false); err != nil {
		return chat.MemoryScope{}, invalidf("%s", err)
	}
	tx, err := s.writeTx(ctx)
	if err != nil {
		return chat.MemoryScope{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_scopes WHERE id LIKE 'shared:%'`).Scan(&n); err != nil {
		return chat.MemoryScope{}, err
	}
	if n >= maxSharedScopes {
		return chat.MemoryScope{}, invalidf("at most %d shared scopes are allowed", maxSharedScopes)
	}
	id := chat.SharedPrefix + slug
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_scopes WHERE id = ?`, id).Scan(&exists); err != nil {
		return chat.MemoryScope{}, err
	}
	if exists > 0 {
		return chat.MemoryScope{}, invalidf("a shared scope named %q already exists", slug)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_scopes(id, name, created_at) VALUES (?,?,?)`, id, name, s.nowMs()); err != nil {
		return chat.MemoryScope{}, err
	}
	if err := tx.Commit(); err != nil {
		return chat.MemoryScope{}, err
	}
	s.publish(id)
	return chat.MemoryScope{ID: id, Name: name, Kind: "shared", Limit: chat.CoreSharedLimit}, nil
}

// DeleteScope removes a scope's entries. Shared scopes disappear (and every bot grant with them);
// a bot's private scope is emptied; the user profile cannot be deleted.
func (s *Store) DeleteScope(ctx context.Context, id string) error {
	switch {
	case id == chat.ScopeUser:
		return invalidf("the user profile cannot be deleted; remove its entries instead")
	case strings.HasPrefix(id, chat.SharedPrefix), strings.HasPrefix(id, chat.BotPrefix):
	default:
		return invalidf("unknown scope %q", id)
	}
	tx, err := s.writeTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := s.scopeName(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE scope = ?`, id); err != nil {
		return err
	}
	if scopeKind(id) == "shared" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM bot_memory_access WHERE scope = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_scopes WHERE id = ?`, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.audit("chat.memory.scope.delete", id, "allowed", "")
	s.publish(id)
	return nil
}

// ListEntries lists entries of a scope, newest first (best match first with a query).
func (s *Store) ListEntries(ctx context.Context, req chat.MemoryListRequest) ([]chat.MemoryEntry, error) {
	if _, err := s.scopeName(ctx, s.db, req.Scope); err != nil {
		return nil, err
	}
	if req.Kind != "" && req.Kind != chat.KindCore && req.Kind != chat.KindFact {
		return nil, invalidf("kind must be core or fact")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	limit = min(limit, maxListLimit)
	var conds []string
	args := []any{}
	from := "memory_entries e"
	order := "e.created_at DESC, e.rowid DESC"
	if q := strings.TrimSpace(req.Query); q != "" {
		fq := ftsQuery(q, false, true)
		if fq == "" {
			return []chat.MemoryEntry{}, nil
		}
		from = "memory_fts JOIN memory_entries e ON e.rowid = memory_fts.rowid"
		conds = append(conds, "memory_fts MATCH ?")
		args = append(args, fq)
		order = "bm25(memory_fts), e.created_at DESC"
	}
	conds = append(conds, "e.scope = ?")
	args = append(args, req.Scope)
	if req.Kind != "" {
		conds = append(conds, "e.kind = ?")
		args = append(args, req.Kind)
	}
	if !req.IncludePending {
		conds = append(conds, "e.pending = 0")
	}
	if !req.IncludeInvalidated {
		conds = append(conds, "e.invalidated_at IS NULL")
	}
	args = append(args, limit)
	cols := strings.ReplaceAll(entryCols, "id, scope, kind, text, trust, author, source_msg_id, pending, created_at, updated_at, COALESCE(invalidated_at, 0)",
		"e.id, e.scope, e.kind, e.text, e.trust, e.author, e.source_msg_id, e.pending, e.created_at, e.updated_at, COALESCE(e.invalidated_at, 0)")
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM `+from+` WHERE `+strings.Join(conds, " AND ")+` ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chat.MemoryEntry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveEntry adds (empty ID) or edits an entry as the user. Edits keep scope and kind; the entry
// becomes trust "user". Core entries respect the scope's character budget.
func (s *Store) SaveEntry(ctx context.Context, req chat.MemorySaveRequest) (chat.MemoryEntry, error) {
	text := collapseSpace(req.Text)
	switch {
	case text == "":
		return chat.MemoryEntry{}, invalidf("text is empty")
	case len([]rune(text)) > maxEntryChars:
		return chat.MemoryEntry{}, invalidf("text is longer than %d characters", maxEntryChars)
	}
	if err := checkUnsafe(text, false); err != nil {
		return chat.MemoryEntry{}, invalidf("%s", err)
	}
	tx, err := s.writeTx(ctx)
	if err != nil {
		return chat.MemoryEntry{}, err
	}
	defer tx.Rollback()
	now := s.nowMs()
	var e chat.MemoryEntry
	if req.ID == "" {
		kind := req.Kind
		if kind == "" {
			kind = chat.KindCore
		}
		if kind != chat.KindCore && kind != chat.KindFact {
			return chat.MemoryEntry{}, invalidf("kind must be core or fact")
		}
		if _, err := s.scopeName(ctx, tx, req.Scope); err != nil {
			return chat.MemoryEntry{}, err
		}
		e = chat.MemoryEntry{Scope: req.Scope, Kind: kind, Text: text, Trust: chat.TrustUser, Author: "user", CreatedAt: now, UpdatedAt: now}
		if kind == chat.KindCore {
			if err := s.checkUserCore(ctx, tx, e.Scope, "", text); err != nil {
				var dup *dupError
				if errors.As(err, &dup) {
					return dup.existing, nil
				}
				return chat.MemoryEntry{}, err
			}
		}
		if e, err = insertEntry(ctx, tx, e, ""); err != nil {
			return chat.MemoryEntry{}, err
		}
	} else {
		if e, err = getEntry(ctx, tx, req.ID); err != nil {
			return chat.MemoryEntry{}, err
		}
		if e.InvalidatedAt != 0 {
			return chat.MemoryEntry{}, invalidf("this entry was replaced and can no longer be edited")
		}
		if req.Scope != "" && req.Scope != e.Scope {
			return chat.MemoryEntry{}, invalidf("an entry cannot move between scopes")
		}
		if req.Kind != "" && req.Kind != e.Kind {
			return chat.MemoryEntry{}, invalidf("an entry cannot change kind")
		}
		if e.Kind == chat.KindCore {
			if err := s.checkUserCore(ctx, tx, e.Scope, e.ID, text); err != nil {
				var dup *dupError
				if errors.As(err, &dup) {
					return chat.MemoryEntry{}, invalidf("another note already says this")
				}
				return chat.MemoryEntry{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memory_entries SET text = ?, trust = 'user', author = 'user', updated_at = ? WHERE id = ?`, text, now, e.ID); err != nil {
			return chat.MemoryEntry{}, err
		}
		e.Text, e.Trust, e.Author, e.UpdatedAt = text, chat.TrustUser, "user", now
	}
	if err := tx.Commit(); err != nil {
		return chat.MemoryEntry{}, err
	}
	s.publish(e.Scope)
	return e, nil
}

type dupError struct{ existing chat.MemoryEntry }

func (*dupError) Error() string { return "duplicate entry" }

// checkUserCore applies the scope budget to a user write; excludeID is the entry being edited.
func (s *Store) checkUserCore(ctx context.Context, q querier, scope, excludeID, text string) error {
	rows, err := effectiveCore(ctx, q, scope)
	if err != nil {
		return err
	}
	var texts []string
	for _, r := range rows {
		if r.ID == excludeID {
			continue
		}
		if squash(r.Text) == squash(text) {
			return &dupError{existing: r.MemoryEntry}
		}
		texts = append(texts, r.Text)
	}
	if after := coreChars(append(texts, text)); after > scopeLimit(scope) {
		return invalidf("this would use %s of %s characters in %s; shorten it or remove another note first",
			commas(after), commas(scopeLimit(scope)), scope)
	}
	return nil
}

// DeleteEntry permanently removes an entry (the user's explicit delete).
func (s *Store) DeleteEntry(ctx context.Context, id string) error {
	e, err := getEntry(ctx, s.db, id)
	if err != nil {
		return err
	}
	tx, err := s.writeTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A pending replacement that pointed at this entry no longer has a target; it stays a plain pending note.
	if _, err := tx.ExecContext(ctx, `UPDATE memory_entries SET invalidated_by = '' WHERE pending = 1 AND invalidated_by = ?`, supersedesPrefix+id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(e.Scope)
	return nil
}

// ResolvePending approves (activates) or rejects (deletes) a held bot write. Approving a core note
// that no longer fits its scope's budget fails; the user frees space first.
func (s *Store) ResolvePending(ctx context.Context, id string, approve bool) error {
	tx, err := s.writeTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	e, err := getEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if !e.Pending || e.InvalidatedAt != 0 {
		return invalidf("this entry is not waiting for approval")
	}
	var by string
	if err := tx.QueryRowContext(ctx, `SELECT invalidated_by FROM memory_entries WHERE id = ?`, id).Scan(&by); err != nil {
		return err
	}
	target := ""
	if strings.HasPrefix(by, supersedesPrefix) {
		target = strings.TrimPrefix(by, supersedesPrefix)
	}
	if !approve {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE id = ?`, id); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.publish(e.Scope)
		return nil
	}
	now := s.nowMs()
	if e.Kind == chat.KindCore {
		// Budget as it will be after this approval: effective entries already include pending ones.
		rows, err := effectiveCore(ctx, tx, e.Scope)
		if err != nil {
			return err
		}
		if used := coreChars(coreTexts(rows)); used > scopeLimit(e.Scope) {
			return invalidf("approving this would use %s of %s characters in %s; remove another note first", commas(used), commas(scopeLimit(e.Scope)), e.Scope)
		}
	}
	if target != "" {
		if err := invalidateEntry(ctx, tx, target, e.Author, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memory_entries SET pending = 0, invalidated_by = '', updated_at = ? WHERE id = ?`, now, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(e.Scope)
	return nil
}
