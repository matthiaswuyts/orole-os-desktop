package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// store is the SQLite persistence for bots, messages, summaries and attachment rows.
type store struct {
	db  *sql.DB
	now func() time.Time
}

const (
	maxAvatarRunes     = 16
	maxDescriptionLen  = 500
	maxReasoningLen    = 32
	maxModelIDLen      = 200
	maxMCPServers      = 50
	maxSkills          = 50
	maxToolParamsBytes = 4 << 10
	maxStepsLimit      = 25
	maxToolCallsLimit  = 100
	maxTaskChoiceLen   = 200
	previewRunes       = 160
)

var (
	colorRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
	providerRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
	scopeRe    = regexp.MustCompile(`^shared:[a-z0-9][a-z0-9-]{0,62}$`)

	knownTools = []string{ToolWebSearch, ToolWebFetch, ToolDatetime, ToolResearch, ToolKnowledge}
)

const botCols = `id, name, avatar, color, description, instructions, provider, model, reasoning, config, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanBot(r scanner) (Bot, error) {
	var b Bot
	var cfg string
	if err := r.Scan(&b.ID, &b.Name, &b.Avatar, &b.Color, &b.Description, &b.Instructions, &b.Provider, &b.Model, &b.Reasoning, &cfg, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return Bot{}, err
	}
	if cfg != "" {
		if err := json.Unmarshal([]byte(cfg), &b.Config); err != nil {
			return Bot{}, errors.New("bot configuration is corrupt: " + err.Error())
		}
	}
	b.Config = normalizeConfig(b.Config)
	return b, nil
}

// normalizeConfig makes every list non-nil so the UI always receives arrays.
func normalizeConfig(c BotConfig) BotConfig {
	if c.Tools == nil {
		c.Tools = []BotTool{}
	}
	if c.MCPServers == nil {
		c.MCPServers = []string{}
	}
	if c.Skills == nil {
		c.Skills = []string{}
	}
	if c.Memory == nil {
		c.Memory = []ScopeAccess{}
	}
	return c
}

func (s *store) listBots(ctx context.Context) ([]Bot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+botCols+` FROM bots WHERE archived_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bots := []Bot{}
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		bots = append(bots, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	prev, err := s.previews(ctx)
	if err != nil {
		return nil, err
	}
	for i := range bots {
		if p, ok := prev[bots[i].ID]; ok {
			bots[i].LastMessage = &p
		}
	}
	slices.SortStableFunc(bots, func(a, b Bot) int {
		ta, tb := a.UpdatedAt, b.UpdatedAt
		if a.LastMessage != nil {
			ta = max(ta, a.LastMessage.At)
		}
		if b.LastMessage != nil {
			tb = max(tb, b.LastMessage.At)
		}
		switch {
		case ta > tb:
			return -1
		case ta < tb:
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return bots, nil
}

// previews returns the latest message of every bot.
func (s *store) previews(ctx context.Context) (map[string]MessagePreview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT bot_id, role, text, parts, created_at FROM messages
		WHERE (bot_id, seq) IN (SELECT bot_id, MAX(seq) FROM messages GROUP BY bot_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MessagePreview{}
	for rows.Next() {
		var id, role, text, parts string
		var at int64
		if err := rows.Scan(&id, &role, &text, &parts, &at); err != nil {
			return nil, err
		}
		out[id] = MessagePreview{Role: role, Text: previewText(text, parts), At: at, Awaiting: awaitingAnswer(role, parts)}
	}
	return out, rows.Err()
}

func (s *store) preview(ctx context.Context, botID string) (*MessagePreview, error) {
	var role, text, parts string
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT role, text, parts, created_at FROM messages WHERE bot_id=? ORDER BY seq DESC LIMIT 1`, botID).Scan(&role, &text, &parts, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &MessagePreview{Role: role, Text: previewText(text, parts), At: at, Awaiting: awaitingAnswer(role, parts)}, nil
}

// awaitingAnswer reports whether a stored message is a bot question that has not been answered (it is still the last message).
func awaitingAnswer(role, parts string) bool {
	if role != "assistant" {
		return false
	}
	for _, p := range RawParts(parts) {
		if p.Type == PartQuestion {
			return true
		}
	}
	return false
}

// previewText is a single line of at most previewRunes characters.
func previewText(text, parts string) string {
	line := strings.Join(strings.Fields(text), " ")
	if line == "" {
		for _, p := range RawParts(parts) {
			if p.Type == PartAttachment {
				line = "Attachment: " + p.Name
				break
			}
		}
	}
	if utf8.RuneCountInString(line) > previewRunes {
		r := []rune(line)
		line = string(r[:previewRunes-1]) + "…"
	}
	return line
}

func (s *store) getBot(ctx context.Context, id string, includeArchived bool) (Bot, error) {
	q := `SELECT ` + botCols + ` FROM bots WHERE id=?`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	b, err := scanBot(s.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Bot{}, notFoundf("Bot not found.")
	}
	if err != nil {
		return Bot{}, err
	}
	b.LastMessage, err = s.preview(ctx, id)
	return b, err
}

// validate normalises a BotInput and enforces every limit. selfID is the bot being saved ("" for new).
func (s *store) validate(ctx context.Context, in BotInput) (BotInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > MaxNameLen {
		return in, invalidf("Name must be 1 to %d characters.", MaxNameLen)
	}
	if !utf8.ValidString(in.Name) || strings.ContainsFunc(in.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return in, invalidf("Name contains invalid characters.")
	}
	in.Avatar = strings.TrimSpace(in.Avatar)
	if utf8.RuneCountInString(in.Avatar) > maxAvatarRunes || !utf8.ValidString(in.Avatar) {
		return in, invalidf("Avatar must be an emoji or up to %d characters.", maxAvatarRunes)
	}
	if in.Color != "" && !colorRe.MatchString(in.Color) {
		return in, invalidf("Color %q is not a valid colour token.", in.Color)
	}
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Description) > maxDescriptionLen {
		return in, invalidf("Description is limited to %d characters.", maxDescriptionLen)
	}
	if utf8.RuneCountInString(in.Instructions) > MaxInstructionsLen || !utf8.ValidString(in.Instructions) {
		return in, invalidf("Instructions are limited to %d characters.", MaxInstructionsLen)
	}
	if (in.Provider == "") != (in.Model == "") {
		return in, invalidf("Choose both a provider and a model, or neither to use the defaults.")
	}
	if in.Provider != "" && !providerRe.MatchString(in.Provider) {
		return in, invalidf("Provider %q is not valid.", in.Provider)
	}
	if len(in.Model) > maxModelIDLen || strings.ContainsFunc(in.Model, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return in, invalidf("Model id is not valid.")
	}
	if len(in.Reasoning) > maxReasoningLen || strings.ContainsFunc(in.Reasoning, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return in, invalidf("Reasoning level is not valid.")
	}
	cfg, err := s.validateConfig(ctx, in.ID, in.Config)
	if err != nil {
		return in, err
	}
	in.Config = cfg
	return in, nil
}

func (s *store) validateConfig(ctx context.Context, selfID string, c BotConfig) (BotConfig, error) {
	tools := make([]BotTool, 0, len(c.Tools))
	for _, t := range c.Tools {
		if !slices.Contains(knownTools, t.ID) {
			return c, invalidf("Unknown tool %q.", t.ID)
		}
		if slices.ContainsFunc(tools, func(x BotTool) bool { return x.ID == t.ID }) {
			continue
		}
		if len(t.Params) > 0 {
			raw, err := json.Marshal(t.Params)
			if err != nil || len(raw) > maxToolParamsBytes {
				return c, invalidf("Settings for tool %q are too large or invalid.", t.ID)
			}
		}
		tools = append(tools, t)
	}
	c.Tools = tools

	servers := make([]string, 0, len(c.MCPServers))
	for _, id := range c.MCPServers {
		if id == "" || len(id) > 200 || strings.ContainsFunc(id, func(r rune) bool { return r < 0x20 || r == '/' }) {
			return c, invalidf("MCP server %q is not a valid server id.", id)
		}
		if !slices.Contains(servers, id) {
			servers = append(servers, id)
		}
	}
	if len(servers) > maxMCPServers {
		return c, invalidf("At most %d integrations can be enabled.", maxMCPServers)
	}
	c.MCPServers = servers

	skills := make([]string, 0, len(c.Skills))
	for _, n := range c.Skills {
		if n == "" || len(n) > 100 || strings.ContainsFunc(n, func(r rune) bool { return r < 0x20 }) {
			return c, invalidf("Skill name %q is not valid.", n)
		}
		if !slices.Contains(skills, n) {
			skills = append(skills, n)
		}
	}
	if len(skills) > maxSkills {
		return c, invalidf("At most %d skills can be enabled.", maxSkills)
	}
	c.Skills = skills

	mem := make([]ScopeAccess, 0, len(c.Memory))
	for _, a := range c.Memory {
		if a.Scope != ScopeUser && !scopeRe.MatchString(a.Scope) {
			return c, invalidf("Memory scope %q is not valid.", a.Scope)
		}
		if a.Write {
			a.Read = true
		}
		if i := slices.IndexFunc(mem, func(x ScopeAccess) bool { return x.Scope == a.Scope }); i >= 0 {
			mem[i].Read = mem[i].Read || a.Read
			mem[i].Write = mem[i].Write || a.Write
			continue
		}
		if a.Scope != ScopeUser {
			var n int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_scopes WHERE id=?`, a.Scope).Scan(&n); err != nil {
				return c, err
			}
			if n == 0 {
				return c, invalidf("Memory scope %q does not exist.", a.Scope)
			}
		}
		mem = append(mem, a)
	}
	c.Memory = mem

	if c.Limits.MaxSteps < 0 || c.Limits.MaxSteps > maxStepsLimit {
		return c, invalidf("Max steps must be between 1 and %d (or 0 for the default).", maxStepsLimit)
	}
	if c.Limits.MaxToolCalls < 0 || c.Limits.MaxToolCalls > maxToolCallsLimit {
		return c, invalidf("Max tool calls must be between 1 and %d (or 0 for the default).", maxToolCallsLimit)
	}

	switch c.ToolAccess {
	case "", ToolAccessAsk, ToolAccessRead, ToolAccessWrite, ToolAccessAll:
	default:
		return c, invalidf("Tool access must be ask, read, write or all (or empty for the global setting).")
	}
	for _, f := range []struct{ label, value string }{
		{"harness", c.Tasks.Harness}, {"model", c.Tasks.Model}, {"reasoning level", c.Tasks.Reasoning},
	} {
		if len(f.value) > maxTaskChoiceLen || !utf8.ValidString(f.value) || strings.ContainsFunc(f.value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return c, invalidf("Code task %s is not valid.", f.label)
		}
	}
	switch c.Tasks.PermissionMode {
	case "", "manual", "auto", "yolo":
	default:
		return c, invalidf("Code task permission mode must be manual, auto or yolo (or empty for the global setting).")
	}
	return normalizeConfig(c), nil
}

// saveBot creates (empty ID) or replaces a bot and keeps bot_memory_access in step with the config.
func (s *store) saveBot(ctx context.Context, in BotInput) (Bot, error) {
	in, err := s.validate(ctx, in)
	if err != nil {
		return Bot{}, err
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Bot{}, err
	}
	defer tx.Rollback()
	if in.ID == "" {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bots WHERE archived_at IS NULL`).Scan(&n); err != nil {
			return Bot{}, err
		}
		if n >= MaxBots {
			return Bot{}, invalidf("You can have at most %d bots. Archive one to add another.", MaxBots)
		}
		in.ID = newID()
		in.Config.Pinned = false
		cfg, err := json.Marshal(in.Config)
		if err != nil {
			return Bot{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bots(`+botCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			in.ID, in.Name, in.Avatar, in.Color, in.Description, in.Instructions, in.Provider, in.Model, in.Reasoning, string(cfg), now, now); err != nil {
			return Bot{}, err
		}
	} else {
		// Pinning is not part of a save: keep what is stored.
		var pinned sql.NullInt64
		_ = tx.QueryRowContext(ctx, `SELECT json_extract(config,'$.pinned') FROM bots WHERE id=? AND archived_at IS NULL`, in.ID).Scan(&pinned)
		in.Config.Pinned = pinned.Valid && pinned.Int64 == 1
		cfg, err := json.Marshal(in.Config)
		if err != nil {
			return Bot{}, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE bots SET name=?, avatar=?, color=?, description=?, instructions=?, provider=?, model=?, reasoning=?, config=?, updated_at=?
			WHERE id=? AND archived_at IS NULL`,
			in.Name, in.Avatar, in.Color, in.Description, in.Instructions, in.Provider, in.Model, in.Reasoning, string(cfg), now, in.ID)
		if err != nil {
			return Bot{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Bot{}, notFoundf("Bot not found.")
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_memory_access WHERE bot_id=?`, in.ID); err != nil {
		return Bot{}, err
	}
	for _, a := range in.Config.Memory {
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_memory_access(bot_id, scope, can_read, can_write) VALUES (?,?,?,?)`, in.ID, a.Scope, a.Read, a.Write); err != nil {
			return Bot{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Bot{}, err
	}
	return s.getBot(ctx, in.ID, false)
}

func (s *store) archiveBot(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE bots SET archived_at=? WHERE id=? AND archived_at IS NULL`, s.now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFoundf("Bot not found.")
	}
	return nil
}

// ---- messages ----

const msgCols = `id, bot_id, seq, role, author, parts, status, usage, error, model, created_at, finished_at`

// finalStatus reports whether a message in this status will not change any more.
func finalStatus(status string) bool {
	switch status {
	case StatusComplete, StatusIncomplete, StatusError, StatusCanceled:
		return true
	}
	return false
}

func scanMessage(r scanner) (Message, error) {
	var m Message
	var parts, usage string
	if err := r.Scan(&m.ID, &m.BotID, &m.Seq, &m.Role, &m.Author, &parts, &m.Status, &usage, &m.Error, &m.Model, &m.CreatedAt, &m.FinishedAt); err != nil {
		return Message{}, err
	}
	m.Parts = RawParts(parts)
	if usage != "" {
		var u MessageUsage
		if json.Unmarshal([]byte(usage), &u) == nil {
			m.Usage = &u
		}
	}
	return m, nil
}

// textProjection is the plain text indexed by FTS: text and report parts.
func textProjection(parts []MsgPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Hidden || (p.Type != PartText && p.Type != PartReport && p.Type != PartQuestion) {
			continue
		}
		if p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

func encodeParts(parts []MsgPart) string {
	if len(parts) == 0 {
		return "[]"
	}
	b, err := json.Marshal(parts)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func encodeUsage(u *MessageUsage) string {
	if u == nil {
		return ""
	}
	b, err := json.Marshal(u)
	if err != nil {
		return ""
	}
	return string(b)
}

const insertMessageSQL = `INSERT INTO messages(` + msgCols + `, text)
	SELECT ?, ?, max(COALESCE((SELECT MAX(seq) FROM messages WHERE bot_id = ?), 0),
	                 COALESCE((SELECT upto_seq FROM memory_consolidation WHERE bot_id = ?), 0)) + 1,
	       ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
	RETURNING seq`

type execer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertMessage stores m with the next sequence number of its bot (monotonic, assigned atomically
// by the statement; it also stays above the consolidation watermark after a clear). A message
// stored already final finished when it was created.
func insertMessage(ctx context.Context, q execer, m *Message) error {
	if m.FinishedAt == 0 && finalStatus(m.Status) {
		m.FinishedAt = m.CreatedAt
	}
	return q.QueryRowContext(ctx, insertMessageSQL,
		m.ID, m.BotID, m.BotID, m.BotID,
		m.Role, m.Author, encodeParts(m.Parts), m.Status, encodeUsage(m.Usage), m.Error, m.Model, m.CreatedAt, m.FinishedAt,
		textProjection(m.Parts),
	).Scan(&m.Seq)
}

func (s *store) insertMessage(ctx context.Context, m *Message) error {
	return insertMessage(ctx, s.db, m)
}

// insertUserMessage stores a user message and claims its attachments for the bot in one transaction.
func (s *store) insertUserMessage(ctx context.Context, m *Message, text string, attachmentIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parts := make([]MsgPart, 0, 1+len(attachmentIDs))
	if text != "" {
		parts = append(parts, MsgPart{Type: PartText, Text: text})
	}
	for _, id := range attachmentIDs {
		var a Attachment
		err := tx.QueryRowContext(ctx, `SELECT id, name, mime, size FROM chat_attachments WHERE id=? AND (bot_id IS NULL OR bot_id=?)`, id, m.BotID).Scan(&a.ID, &a.Name, &a.MIME, &a.Size)
		if errors.Is(err, sql.ErrNoRows) {
			return invalidf("An attachment is missing or has expired. Attach the file again.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE chat_attachments SET bot_id=? WHERE id=?`, m.BotID, id); err != nil {
			return err
		}
		parts = append(parts, MsgPart{Type: PartAttachment, AttachmentID: a.ID, Name: a.Name, MIME: a.MIME, Size: a.Size})
	}
	m.Parts = parts
	if err := insertMessage(ctx, tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

// updateMessage stores m's content and status. finished_at is written only while it is still 0, so
// the first final snapshot wins.
func (s *store) updateMessage(ctx context.Context, m Message) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET parts=?, text=?, status=?, usage=?, error=?, model=?,
		finished_at = CASE WHEN finished_at = 0 THEN ? ELSE finished_at END WHERE id=?`,
		encodeParts(m.Parts), textProjection(m.Parts), m.Status, encodeUsage(m.Usage), m.Error, m.Model, m.FinishedAt, m.ID)
	return err
}

func (s *store) setStatus(ctx context.Context, id, status, errText string) error {
	_, err := s.setStatusAt(ctx, id, status, errText, s.now().UnixMilli())
	return err
}

// setStatusAt moves a message to status. A final status records at as its finish time unless it
// already has one; any other status clears it. It returns the stored finish time.
func (s *store) setStatusAt(ctx context.Context, id, status, errText string, at int64) (int64, error) {
	if !finalStatus(status) {
		at = 0
	}
	var finished int64
	err := s.db.QueryRowContext(ctx, `UPDATE messages SET status=?, error=?,
		finished_at = CASE WHEN ? = 0 OR finished_at = 0 THEN ? ELSE finished_at END WHERE id=? RETURNING finished_at`,
		status, errText, at, at, id).Scan(&finished)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return finished, err
}

func (s *store) getMessage(ctx context.Context, id string) (Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, notFoundf("Message not found.")
	}
	return m, err
}

func (s *store) lastMessage(ctx context.Context, botID string) (Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE bot_id=? ORDER BY seq DESC LIMIT 1`, botID))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, notFoundf("This conversation has no messages.")
	}
	return m, err
}

// userMessageBefore returns the latest user message with seq below seq.
func (s *store) userMessageBefore(ctx context.Context, botID string, seq int64) (Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE bot_id=? AND role='user' AND seq<? ORDER BY seq DESC LIMIT 1`, botID, seq))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, notFoundf("There is no message to answer.")
	}
	return m, err
}

// resetMessage empties an assistant message so it can be regenerated in place (same id and seq,
// so the UI's upsert replaces the old reply).
func (s *store) resetMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET parts='[]', text='', status=?, usage='', error='', model='', finished_at=0 WHERE id=?`, StatusRunning, id)
	return err
}

func (s *store) pageMessages(ctx context.Context, req MessagesRequest) (MessagePage, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages WHERE bot_id=? AND (?=0 OR seq<?) ORDER BY seq DESC LIMIT ?`,
		req.BotID, req.BeforeSeq, req.BeforeSeq, limit+1)
	if err != nil {
		return MessagePage{}, err
	}
	defer rows.Close()
	msgs := make([]Message, 0, min(limit+1, 64))
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return MessagePage{}, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{HasMore: len(msgs) > limit}
	if page.HasMore {
		msgs = msgs[:limit]
	}
	slices.Reverse(msgs)
	page.Messages = msgs
	return page, nil
}

// clearConversation deletes messages, the summary and the bot's sent attachments (except files kept
// as knowledge). It returns the blob hashes that may have become unreferenced.
func (s *store) clearConversation(ctx context.Context, botID string) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE bot_id=?`, botID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_summaries WHERE bot_id=?`, botID); err != nil {
		return nil, err
	}
	shas, err := deleteAttachmentRows(ctx, tx, `bot_id=?`, botID)
	if err != nil {
		return nil, err
	}
	return shas, tx.Commit()
}

type summary struct {
	Text    string
	UptoSeq int64
}

func (s *store) getSummary(ctx context.Context, botID string) (summary, error) {
	var sm summary
	err := s.db.QueryRowContext(ctx, `SELECT text, upto_seq FROM conversation_summaries WHERE bot_id=?`, botID).Scan(&sm.Text, &sm.UptoSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return summary{}, nil
	}
	return sm, err
}

func (s *store) putSummary(ctx context.Context, botID string, sm summary) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_summaries(bot_id, upto_seq, text, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(bot_id) DO UPDATE SET upto_seq=excluded.upto_seq, text=excluded.text, updated_at=excluded.updated_at`,
		botID, sm.UptoSeq, sm.Text, s.now().UnixMilli())
	return err
}

// markInterrupted runs at startup: replies that were streaming when the daemon stopped become
// incomplete; queued user messages that will never run become canceled. Both finish now.
func (s *store) markInterrupted(ctx context.Context) error {
	now := s.now().UnixMilli()
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET status=?, finished_at=? WHERE status=?`, StatusIncomplete, now, StatusRunning); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET status=?, error=?, finished_at=? WHERE status=?`,
		StatusCanceled, "Not sent: Orole was closed before this message was processed.", now, StatusPending)
	return err
}

// setPinned pins or unpins a bot without touching the rest of its configuration.
func (s *store) setPinned(ctx context.Context, id string, pinned bool) error {
	expr := "json_remove(config, '$.pinned')"
	if pinned {
		expr = "json_set(config, '$.pinned', json('true'))"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE bots SET config=`+expr+`, updated_at=? WHERE id=? AND archived_at IS NULL`, s.now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFoundf("Bot not found.")
	}
	return nil
}
