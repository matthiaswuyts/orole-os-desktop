package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// Skill proposals: a bot may suggest a reusable skill, but the proposal is only a database row until
// the user approves it. Approval writes it into the central registry (which syncs it to coding
// agents), under a name prefixed with the bot's slug, with the source "chat:<bot id>".
const (
	maxPendingProposals = 3
	maxProposalBody     = 32 << 10
	maxProposalDesc     = 300
	maxBotSlug          = 20
)

// botSlug turns a bot name into a registry-safe prefix.
func botSlug(name string) string {
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
		if b.Len() >= maxBotSlug {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "bot"
	}
	return s
}

func skillSource(botID string) string { return "chat:" + botID }

// composeSkill renders SKILL.md. The description is single-quoted YAML, which the registry's
// frontmatter reader handles without escape sequences.
func composeSkill(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: '" + strings.ReplaceAll(description, "'", "''") + "'\n---\n\n" + strings.TrimSpace(body) + "\n"
}

// Propose validates and stores a pending proposal for bot.
func (s *Store) Propose(ctx context.Context, bot chat.Bot, name, description, body string) (chat.SkillProposal, error) {
	if s.cfg.Skills == nil {
		return chat.SkillProposal{}, invalidf("the skills registry is unavailable")
	}
	slug := botSlug(bot.Name)
	short := strings.ToLower(strings.TrimSpace(name))
	short = strings.TrimPrefix(short, slug+"-")
	if err := skills.ValidName(short); err != nil {
		return chat.SkillProposal{}, invalidf("%s", strings.TrimPrefix(err.Error(), skills.ErrInvalid.Error()+": "))
	}
	full := slug + "-" + short
	if err := skills.ValidName(full); err != nil {
		return chat.SkillProposal{}, invalidf("name is too long once prefixed with %q; use at most %d characters", slug+"-", skills.MaxNameLen-len(slug)-1)
	}
	desc := oneLine(description)
	body = strings.TrimSpace(body)
	switch {
	case desc == "":
		return chat.SkillProposal{}, invalidf("description is required")
	case len([]rune(desc)) > maxProposalDesc:
		return chat.SkillProposal{}, invalidf("description is longer than %d characters", maxProposalDesc)
	case body == "":
		return chat.SkillProposal{}, invalidf("body is required")
	case len(body) > maxProposalBody:
		return chat.SkillProposal{}, invalidf("body is larger than %d KiB", maxProposalBody>>10)
	case strings.HasPrefix(body, "---"):
		return chat.SkillProposal{}, invalidf("body must be plain Markdown instructions without a frontmatter block; name and description are added for you")
	}
	if err := checkUnsafe(desc, true); err != nil {
		return chat.SkillProposal{}, invalidf("description %s", err)
	}
	if err := checkUnsafe(body, true); err != nil {
		return chat.SkillProposal{}, invalidf("body %s", err)
	}
	text := composeSkill(full, desc, body)
	meta, _, err := skills.ParseFrontmatter(text)
	if err != nil || meta.Name != full || meta.Description != desc || len(text) > skills.MaxSkillMD {
		return chat.SkillProposal{}, invalidf("the skill could not be rendered as a valid SKILL.md")
	}

	tx, err := s.writeTx(ctx)
	if err != nil {
		return chat.SkillProposal{}, err
	}
	defer tx.Rollback()
	var pending, same int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(name = ?), 0) FROM skill_proposals WHERE bot_id = ? AND status = 'pending'`, full, bot.ID).Scan(&pending, &same); err != nil {
		return chat.SkillProposal{}, err
	}
	if same > 0 {
		return chat.SkillProposal{}, invalidf("a proposal named %q is already waiting for the user's approval", full)
	}
	if pending >= maxPendingProposals {
		return chat.SkillProposal{}, invalidf("%d skill proposals are already waiting for the user's approval; wait for a decision before proposing more", pending)
	}
	p := chat.SkillProposal{ID: newID(), BotID: bot.ID, Name: full, Description: desc, Body: body, Status: "pending", CreatedAt: s.nowMs()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_proposals(id, bot_id, name, description, body, status, created_at) VALUES (?,?,?,?,?,'pending',?)`,
		p.ID, p.BotID, p.Name, p.Description, p.Body, p.CreatedAt); err != nil {
		return chat.SkillProposal{}, err
	}
	return p, tx.Commit()
}

// Proposals lists pending proposals, newest first; botID "" lists every bot's.
func (s *Store) Proposals(ctx context.Context, botID string) ([]chat.SkillProposal, error) {
	q := `SELECT id, bot_id, name, description, body, status, created_at FROM skill_proposals WHERE status = 'pending'`
	var args []any
	if botID != "" {
		q += ` AND bot_id = ?`
		args = append(args, botID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chat.SkillProposal{}
	for rows.Next() {
		var p chat.SkillProposal
		if err := rows.Scan(&p.ID, &p.BotID, &p.Name, &p.Description, &p.Body, &p.Status, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ResolveSkill approves (saving into the registry) or rejects a pending proposal.
func (s *Store) ResolveSkill(ctx context.Context, id string, approve bool) error {
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	var p chat.SkillProposal
	err := s.db.QueryRowContext(ctx, `SELECT id, bot_id, name, description, body, status, created_at FROM skill_proposals WHERE id = ?`, id).
		Scan(&p.ID, &p.BotID, &p.Name, &p.Description, &p.Body, &p.Status, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundf("skill proposal not found")
	}
	if err != nil {
		return err
	}
	if p.Status != "pending" {
		return invalidf("this proposal was already %s", p.Status)
	}
	status := "rejected"
	if approve {
		if s.cfg.Skills == nil {
			return invalidf("the skills registry is unavailable")
		}
		// Re-check at the gate: the row must still satisfy the rules it was accepted under.
		if err := checkUnsafe(p.Body, true); err != nil {
			return invalidf("proposal %s", err)
		}
		if existing, err := s.cfg.Skills.Get(p.Name); err == nil && existing.Source != skillSource(p.BotID) {
			return invalidf("a skill named %q already exists in the registry and was not created by this bot; reject this proposal and ask the bot to pick another name", p.Name)
		} else if err != nil && !errors.Is(err, skills.ErrNotFound) {
			return invalidf("%s", err)
		}
		sk, err := s.cfg.Skills.Save(p.Name, composeSkill(p.Name, p.Description, p.Body), skillSource(p.BotID))
		if err != nil {
			s.audit("chat.skills.approve", p.Name, "failed", err.Error())
			if errors.Is(err, skills.ErrInvalid) {
				return invalidf("%s", err)
			}
			return err
		}
		s.audit("chat.skills.approve", sk.Name, "allowed", skillSource(p.BotID))
		status = "approved"
	} else {
		s.audit("chat.skills.reject", p.Name, "allowed", skillSource(p.BotID))
	}
	_, err = s.db.ExecContext(ctx, `UPDATE skill_proposals SET status = ?, decided_at = ? WHERE id = ? AND status = 'pending'`, status, s.nowMs(), id)
	return err
}

type proposeSkillTool struct{ s *Store }

func (proposeSkillTool) ID() string { return "propose_skill" }

func (t proposeSkillTool) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools || t.s.cfg.Skills == nil {
		return nil
	}
	return []llm.ToolSpec{{
		Name: "propose_skill",
		Description: "Suggest a reusable skill (a short Markdown playbook) when you worked out a procedure the user is likely to repeat. " +
			"The user must approve it before it exists; nothing happens until then. Do not include secrets, credentials or commands that download and run code.",
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Lowercase slug with hyphens, e.g. weekly-report"},"description":{"type":"string","description":"One sentence on when to use the skill."},"body":{"type":"string","description":"Markdown instructions, without frontmatter."}},"required":["name","description","body"]}`),
	}}
}

func (t proposeSkillTool) Call(ctx context.Context, rc chat.RunContext, _ string, raw json.RawMessage) (chat.ToolResult, error) {
	var a struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolFail("invalid arguments: " + err.Error()), nil
	}
	bot, err := t.s.loadBot(ctx, t.s.db, runBotID(rc))
	if err != nil {
		return userToolResult(err)
	}
	p, err := t.s.Propose(ctx, bot, a.Name, a.Description, a.Body)
	if err != nil {
		return userToolResult(err)
	}
	if rc.Emit != nil {
		rc.Emit(chat.MsgPart{Type: chat.PartNotice, Text: fmt.Sprintf("Skill proposed: %s (waiting for your approval)", p.Name)})
	}
	return chat.ToolResult{Text: fmt.Sprintf("Proposal %q saved. It is inert until the user approves it; do not rely on it yet.", p.Name)}, nil
}
