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

// Skill proposals: a bot may offer a reusable skill, shown to the user as a card in the conversation.
// The proposal is only a database row until the user presses Add on the card; then ResolveSkill saves
// it into the central registry (which syncs it to coding agents) and gives it to the proposing bot, so
// the bot can use it at once. Dismissing the card rejects it. The registry skill carries the source
// "chat:<bot id>", which lets the same bot update its own skill later and nobody else's.
const (
	maxPendingProposals = 10
	maxProposalBody     = 32 << 10
	maxProposalDesc     = 1024
)

func skillSource(botID string) string { return "chat:" + botID }

// composeSkill renders SKILL.md. The description is single-quoted YAML, which the registry's
// frontmatter reader handles without escape sequences.
func composeSkill(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: '" + strings.ReplaceAll(description, "'", "''") + "'\n---\n\n" + strings.TrimSpace(body) + "\n"
}

// SkillGranter gives a bot a registry skill it can read. chat.Manager implements it.
type SkillGranter interface {
	GrantSkill(ctx context.Context, botID, name string) error
}

// checkNameFree rejects a name that belongs to someone else's registry skill and reports whether the
// bot is updating a skill it created before.
func (s *Store) checkNameFree(name, botID string) (updates bool, err error) {
	existing, err := s.cfg.Skills.Get(name)
	switch {
	case errors.Is(err, skills.ErrNotFound):
		return false, nil
	case err != nil:
		return false, invalidf("%s", err)
	case existing.Source != skillSource(botID):
		return false, invalidf("a skill named %q already exists and was not created by you; choose a different name", name)
	}
	return true, nil
}

// Propose validates and stores a pending proposal for bot. A newer proposal under the same name
// replaces the bot's earlier pending one, so a revised draft never conflicts with itself. The result
// says whether it updates a skill the bot created before.
func (s *Store) Propose(ctx context.Context, bot chat.Bot, name, description, body string) (p chat.SkillProposal, updates bool, err error) {
	if s.cfg.Skills == nil {
		return p, false, invalidf("the skills registry is unavailable")
	}
	full := strings.ToLower(strings.TrimSpace(name))
	if err := skills.ValidName(full); err != nil {
		return p, false, invalidf("%s", strings.TrimPrefix(err.Error(), skills.ErrInvalid.Error()+": "))
	}
	desc := oneLine(description)
	body = strings.TrimSpace(body)
	switch {
	case desc == "":
		return p, false, invalidf("description is required")
	case len([]rune(desc)) > maxProposalDesc:
		return p, false, invalidf("description is longer than %d characters; the first 300 are what you see in your skill list, so put the trigger there", maxProposalDesc)
	case body == "":
		return p, false, invalidf("body is required")
	case len(body) > maxProposalBody:
		return p, false, invalidf("body is larger than %d KiB", maxProposalBody>>10)
	case strings.HasPrefix(body, "---"):
		return p, false, invalidf("body must be plain Markdown instructions without a frontmatter block; name and description are added for you")
	}
	if err := checkUnsafe(desc, true); err != nil {
		return p, false, invalidf("description %s", err)
	}
	if err := checkUnsafe(body, true); err != nil {
		return p, false, invalidf("body %s", err)
	}
	text := composeSkill(full, desc, body)
	meta, _, err := skills.ParseFrontmatter(text)
	if err != nil || meta.Name != full || meta.Description != desc || len(text) > skills.MaxSkillMD {
		return p, false, invalidf("the skill could not be rendered as a valid SKILL.md")
	}
	if updates, err = s.checkNameFree(full, bot.ID); err != nil {
		return p, false, err
	}

	tx, err := s.writeTx(ctx)
	if err != nil {
		return p, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_proposals WHERE bot_id = ? AND name = ? AND status = 'pending'`, bot.ID, full); err != nil {
		return p, false, err
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_proposals WHERE bot_id = ? AND status = 'pending'`, bot.ID).Scan(&pending); err != nil {
		return p, false, err
	}
	if pending >= maxPendingProposals {
		return p, false, invalidf("%d skill proposals are already waiting for the user's decision; wait before proposing more", pending)
	}
	p = chat.SkillProposal{ID: newID(), BotID: bot.ID, Name: full, Description: desc, Body: body, Status: "pending", CreatedAt: s.nowMs()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_proposals(id, bot_id, name, description, body, status, created_at) VALUES (?,?,?,?,?,'pending',?)`,
		p.ID, p.BotID, p.Name, p.Description, p.Body, p.CreatedAt); err != nil {
		return p, false, err
	}
	return p, updates, tx.Commit()
}

// Proposal returns one proposal in any state, which is how a card in the conversation learns what became of it.
func (s *Store) Proposal(ctx context.Context, id string) (chat.SkillProposal, error) {
	var p chat.SkillProposal
	err := s.db.QueryRowContext(ctx, `SELECT id, bot_id, name, description, body, status, created_at FROM skill_proposals WHERE id = ?`, id).
		Scan(&p.ID, &p.BotID, &p.Name, &p.Description, &p.Body, &p.Status, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, notFoundf("skill proposal not found")
	}
	return p, err
}

// ResolveSkill adds a pending proposal to the registry and gives it to the proposing bot (approve), or
// rejects it.
func (s *Store) ResolveSkill(ctx context.Context, id string, approve bool) error {
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	p, err := s.Proposal(ctx, id)
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
		if _, err := s.checkNameFree(p.Name, p.BotID); err != nil {
			return err
		}
		sk, err := s.cfg.Skills.Save(p.Name, composeSkill(p.Name, p.Description, p.Body), skillSource(p.BotID))
		if err != nil {
			s.audit("chat.skills.approve", p.Name, "failed", err.Error())
			if errors.Is(err, skills.ErrInvalid) {
				return invalidf("%s", err)
			}
			return err
		}
		if s.cfg.Grants != nil {
			if err := s.cfg.Grants.GrantSkill(ctx, p.BotID, sk.Name); err != nil {
				s.audit("chat.skills.approve", sk.Name, "failed", "grant: "+err.Error())
				return invalidf("the skill was saved, but it could not be added to the bot: %s", err)
			}
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
		Description: "Offer the user a new skill, or an update to one you created, as a card in the chat with an Add button. " +
			"Use it once the draft is agreed (or when you worked out a procedure the user will repeat). Nothing exists until the user presses Add; " +
			"then the skill is saved and given to you. Calling it again with the same name replaces your earlier draft. " +
			"Do not include secrets, credentials or commands that download and run code.",
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Lowercase slug with hyphens, exactly as it should be saved, e.g. weekly-report"},"description":{"type":"string","description":"When to use the skill and what it does; the trigger goes first. At most 1024 characters."},"body":{"type":"string","description":"Markdown instructions, without frontmatter."}},"required":["name","description","body"]}`),
	}}
}

// skillCardSpec is the JSON of the `skill` ui part the chat draws as a card.
type skillCardSpec struct {
	ProposalID  string `json:"proposalId"`
	BotID       string `json:"botId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Updates     bool   `json:"updates,omitempty"`
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
	p, updates, err := t.s.Propose(ctx, bot, a.Name, a.Description, a.Body)
	if err != nil {
		return userToolResult(err)
	}
	spec, err := json.Marshal(skillCardSpec{ProposalID: p.ID, BotID: p.BotID, Name: p.Name, Description: p.Description, Body: p.Body, Updates: updates})
	if err != nil {
		return toolFail("the skill card could not be built"), nil
	}
	return chat.ToolResult{
		Text: fmt.Sprintf("The user now sees a card for the skill %q with an Add button. Nothing exists until they press it, so do not say it was added. "+
			"Keep your reply short: say what the skill does in one sentence. If they ask for changes, call propose_skill again with the same name.", p.Name),
		Parts: []chat.MsgPart{{Type: chat.PartUI, ID: "propose_skill", Name: "skill", Spec: string(spec)}},
	}, nil
}
