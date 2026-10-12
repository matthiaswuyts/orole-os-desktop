package automations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Proposals: a bot may draft an automation in chat. The draft is validated like a builder
// save, stored as an immutable row, and shown to the user as a card. Nothing runs until the
// user approves the card; ResolveProposal then saves exactly the stored definition, with the
// bot recorded as its creator. A bot never saves or enables an automation itself.

const (
	maxPendingProposals = 10
	maxRationale        = 500
)

var (
	// ErrInvalid marks a refused proposal or decision; the message is written for the user and the bot.
	ErrInvalid = errors.New("invalid automation proposal")
	// ErrChanged marks an edit whose automation changed after the bot drafted it.
	ErrChanged = errors.New("automation changed")
)

type proposalError struct {
	kind error
	msg  string
}

func (e *proposalError) Error() string        { return e.msg }
func (e *proposalError) Is(target error) bool { return target == e.kind }

func invalidf(format string, args ...any) error {
	return &proposalError{kind: ErrInvalid, msg: fmt.Sprintf(format, args...)}
}

// Proposal is an automation a bot drafted; inert until the user approves it.
type Proposal struct {
	ID    string `json:"id"`
	BotID string `json:"botId"`
	// Replaces is the automation an edit applies to; "" for a new automation.
	Replaces string `json:"replaces"`
	// ReplacesEnabled reports whether the automation being replaced is turned on now.
	ReplacesEnabled bool       `json:"replacesEnabled"`
	Rationale       string     `json:"rationale"`
	Definition      Definition `json:"definition"`
	// Risks are effects the user must review in the builder: a draft with any can only be
	// added turned off.
	Risks        []string `json:"risks"`
	Status       string   `json:"status"` // pending | approved | rejected | replaced
	AutomationID string   `json:"automationId"`
	CreatedAt    int64    `json:"createdAt"`
	DecidedAt    int64    `json:"decidedAt"`
}

// ProposalDecision is the user's answer to a proposal card. Enable turns the automation on
// as it is saved; it is refused for drafts with Risks.
type ProposalDecision struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve"`
	Enable  bool   `json:"enable"`
}

// ProposeResult is a stored proposal, or the issues that kept the draft from being stored.
type ProposeResult struct {
	Proposal Proposal `json:"proposal"`
	Issues   []Issue  `json:"issues"`
}

// Propose validates d as a draft by botID and stores it as a pending proposal. replaces names
// an existing automation the draft edits. Validation problems come back as Issues (nothing is
// stored), so the bot can fix the draft. A newer draft for the same target (the replaced
// automation, or the same name) replaces the bot's earlier pending one.
func (e *Engine) Propose(ctx context.Context, botID string, d Definition, rationale, replaces string) (ProposeResult, error) {
	if botID == "" {
		return ProposeResult{}, invalidf("only a bot can draft an automation")
	}
	rationale = strings.Join(strings.Fields(rationale), " ")
	if len([]rune(rationale)) > maxRationale {
		return ProposeResult{}, invalidf("rationale is longer than %d characters", maxRationale)
	}
	var base string
	if replaces != "" {
		current, version, err := e.storedDefinition(ctx, replaces)
		if err != nil {
			return ProposeResult{}, err
		}
		base = version
		if strings.TrimSpace(d.Name) == "" {
			d.Name = current.Name
		}
	}
	d.ID = replaces
	d.Enabled = false
	d = Normalize(Upgrade(d))
	stored := d
	if stored.ID == "" {
		stored.ID = "draft" // storable checks the size of the document as it will be saved
	}
	_, issues, err := e.storable(stored)
	if err != nil || len(issues) > 0 {
		return ProposeResult{Issues: issues}, err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return ProposeResult{}, err
	}

	e.proposalMu.Lock()
	defer e.proposalMu.Unlock()
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return ProposeResult{}, err
	}
	defer tx.Rollback()
	now := e.clock.Now().UnixMilli()
	target, value := "name", d.Name
	if replaces != "" {
		target, value = "replaces", replaces
	}
	if _, err := tx.ExecContext(ctx, `UPDATE automation_proposals SET status='replaced', decided_at=? WHERE bot_id=? AND status='pending' AND `+target+`=?`, now, botID, value); err != nil {
		return ProposeResult{}, err
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM automation_proposals WHERE bot_id=? AND status='pending'`, botID).Scan(&pending); err != nil {
		return ProposeResult{}, err
	}
	if pending >= maxPendingProposals {
		return ProposeResult{}, invalidf("%d automation drafts are already waiting for the user's decision; wait before proposing more", pending)
	}
	p := Proposal{ID: uuid.NewString(), BotID: botID, Replaces: replaces, Rationale: rationale, Definition: d, Status: "pending", CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO automation_proposals(id,bot_id,name,replaces,base_version,rationale,definition,status,created_at) VALUES(?,?,?,?,?,?,?,'pending',?)`,
		p.ID, botID, d.Name, replaces, base, rationale, string(raw), now); err != nil {
		return ProposeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProposeResult{}, err
	}
	e.audit("automation.proposal.create", p.ID, "allowed", "bot:"+botID)
	return ProposeResult{Proposal: e.describeProposal(ctx, p), Issues: []Issue{}}, nil
}

// Proposal returns one proposal in any state; the chat card asks what became of its draft.
func (e *Engine) Proposal(ctx context.Context, id string) (Proposal, error) {
	p, _, err := e.loadProposal(ctx, id)
	if err != nil {
		return p, err
	}
	return e.describeProposal(ctx, p), nil
}

// ResolveProposal saves a pending proposal as the user approved it, or rejects it. The stored
// definition is checked again at this gate: it must still validate and contain no secrets, an
// edit must still apply to the automation version the bot saw, and a draft with risks is only
// ever saved turned off.
func (e *Engine) ResolveProposal(ctx context.Context, req ProposalDecision) (Proposal, error) {
	e.proposalMu.Lock()
	defer e.proposalMu.Unlock()
	p, base, err := e.loadProposal(ctx, req.ID)
	if err != nil {
		return p, err
	}
	if p.Status != "pending" {
		return p, invalidf("this draft was already %s", p.Status)
	}
	now := e.clock.Now().UnixMilli()
	if !req.Approve {
		if _, err := e.db.ExecContext(ctx, `UPDATE automation_proposals SET status='rejected', decided_at=? WHERE id=? AND status='pending'`, now, p.ID); err != nil {
			return p, err
		}
		e.audit("automation.proposal.reject", p.ID, "allowed", "bot:"+p.BotID)
		p.Status, p.DecidedAt = "rejected", now
		return e.describeProposal(ctx, p), nil
	}
	d := p.Definition
	risks := Risks(d)
	if req.Enable && len(risks) > 0 {
		return p, invalidf("this automation has steps to review first; add it turned off and check it in Automations")
	}
	d.Enabled = req.Enable
	if p.Replaces != "" {
		_, version, err := e.storedDefinition(ctx, p.Replaces)
		if err != nil {
			return p, err
		}
		if version != base {
			return p, &proposalError{kind: ErrChanged, msg: "the automation changed after this draft was made; ask the bot for a new draft"}
		}
	}
	res, err := e.save(ctx, d, "bot:"+p.BotID)
	if err != nil {
		return p, err
	}
	if !res.Saved {
		msg := "the draft no longer validates"
		if len(res.Issues) > 0 {
			msg = fmt.Sprintf("the draft no longer validates: %s: %s", res.Issues[0].Path, res.Issues[0].Message)
		}
		e.audit("automation.proposal.approve", p.ID, "failed", msg)
		return p, invalidf("%s", msg)
	}
	if _, err := e.db.ExecContext(ctx, `UPDATE automation_proposals SET status='approved', automation_id=?, decided_at=? WHERE id=? AND status='pending'`, res.Definition.ID, now, p.ID); err != nil {
		return p, err
	}
	e.audit("automation.proposal.approve", p.ID, "allowed", "bot:"+p.BotID+" -> "+res.Definition.ID)
	p.Status, p.AutomationID, p.DecidedAt = "approved", res.Definition.ID, now
	p.Definition = res.Definition // what was saved: the card reports whether it is on
	return e.describeProposal(ctx, p), nil
}

// loadProposal reads a proposal row and the version of the automation it edits.
func (e *Engine) loadProposal(ctx context.Context, id string) (Proposal, string, error) {
	var p Proposal
	var raw, base string
	err := e.db.QueryRowContext(ctx, `SELECT id,bot_id,replaces,base_version,rationale,definition,status,automation_id,created_at,decided_at FROM automation_proposals WHERE id=?`, id).
		Scan(&p.ID, &p.BotID, &p.Replaces, &base, &p.Rationale, &raw, &p.Status, &p.AutomationID, &p.CreatedAt, &p.DecidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, "", fmt.Errorf("%w: automation draft", ErrNotFound)
	}
	if err != nil {
		return p, "", err
	}
	if err := json.Unmarshal([]byte(raw), &p.Definition); err != nil {
		return p, "", err
	}
	return p, base, nil
}

// describeProposal fills the derived fields of a proposal. For an approved draft,
// Definition.Enabled is the saved automation's current switch.
func (e *Engine) describeProposal(ctx context.Context, p Proposal) Proposal {
	p.Risks = Risks(p.Definition)
	if p.Replaces != "" && p.Status == "pending" {
		if current, _, err := e.storedDefinition(ctx, p.Replaces); err == nil {
			p.ReplacesEnabled = current.Enabled
		}
	}
	if p.Status == "approved" && p.AutomationID != "" {
		if current, _, err := e.storedDefinition(ctx, p.AutomationID); err == nil {
			p.Definition.Enabled = current.Enabled
		}
	}
	return p
}

// storedDefinition reads a saved automation and its version: a hash of the stored document
// without its on/off switch, so turning the automation on or off does not outdate a draft.
func (e *Engine) storedDefinition(ctx context.Context, id string) (Definition, string, error) {
	var raw string
	err := e.db.QueryRowContext(ctx, `SELECT definition FROM automations WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Definition{}, "", fmt.Errorf("%w: automation %s", ErrNotFound, id)
	}
	if err != nil {
		return Definition{}, "", err
	}
	var d Definition
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Definition{}, "", err
	}
	d = Upgrade(d)
	versioned := d
	versioned.Enabled = false
	b, err := json.Marshal(versioned)
	if err != nil {
		return Definition{}, "", err
	}
	sum := sha256.Sum256(b)
	return d, hex.EncodeToString(sum[:]), nil
}

// Risks lists the effects of d that the user must review in the builder before it runs:
// programs it executes and requests it sends outside Orole. Policy still checks each of
// them at run time; this is the gate at creation.
func Risks(d Definition) []string {
	out := []string{}
	if len(d.AllowedExecutables) > 0 {
		out = append(out, "Allows running "+strings.Join(d.AllowedExecutables, ", "))
	}
	if len(d.AllowedHosts) > 0 {
		out = append(out, "Allows requests to "+strings.Join(d.AllowedHosts, ", "))
	}
	walk(d.Steps, "steps", 0, func(s Step, _ string, _ int) {
		var p map[string]any
		_ = json.Unmarshal(s.Params, &p)
		switch s.Type {
		case "command.run", "test.run":
			cmd := strings.TrimSpace(str(p, "executable") + " " + strings.Join(stringList(p["args"]), " "))
			out = append(out, fmt.Sprintf("Runs %s in %s", cmd, str(p, "dir")))
		case "http.request":
			out = append(out, fmt.Sprintf("Sends %s %s", strings.ToUpper(str(p, "method")), str(p, "url")))
		}
	})
	return out
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
