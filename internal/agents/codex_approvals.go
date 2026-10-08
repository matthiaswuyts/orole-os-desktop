package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// codexPermInputCap bounds the exact input shown for one approval; bigger requests cannot be
// shown honestly and are declined.
const codexPermInputCap = 64 << 10

// codexPending is a server request waiting for the user's decision. allow and deny are the
// complete JSON-RPC results to send for each answer (nil allow = Codex offered no one-time approval).
type codexPending struct {
	rpcID  json.RawMessage
	rpcKey string
	allow  any
	deny   any
}

// serverRequest handles requests Codex sends to its client. Everything that blocks the agent is
// either shown to the user (EvPermission) or answered immediately and visibly.
func (p *codexProc) serverRequest(m rpcMsg) {
	switch m.Method {
	case "item/commandExecution/requestApproval":
		p.commandApproval(m)
	case "item/fileChange/requestApproval":
		p.fileChangeApproval(m)
	case "item/permissions/requestApproval":
		p.permissionsApproval(m)
	case "applyPatchApproval":
		p.legacyPatchApproval(m)
	case "execCommandApproval":
		p.legacyExecApproval(m)
	case "mcpServer/elicitation/request":
		p.elicitation(m)
	case "item/tool/requestUserInput":
		p.userInput(m)
	default:
		// account token refresh, attestation, dynamic tools: Orole offers none of them.
		p.replyError(m.ID, -32601, "Orole does not handle "+m.Method)
	}
}

// verdict is the result body of a command or file-change approval.
func verdict(decision string) any { return map[string]any{"decision": decision} }

// ask registers a pending request and shows it to the user.
func (p *codexProc) ask(m rpcMsg, tool, summary string, input json.RawMessage, allow, deny any) {
	id := uuid.NewString()
	p.mu.Lock()
	p.pending[id] = &codexPending{rpcID: m.ID, rpcKey: string(m.ID), allow: allow, deny: deny}
	p.byRPC[string(m.ID)] = id
	p.mu.Unlock()
	p.emit(Event{Kind: EvPermission, Text: tool, Data: jraw(PermissionRequest{
		ID: id, Tool: tool, Summary: oneLine(summary, 400), Input: input, InputJSON: string(input), At: time.Now().UTC()})})
}

// refuse answers a request Orole cannot show honestly and tells the user why.
func (p *codexProc) refuse(m rpcMsg, deny any, why string) {
	_ = p.reply(m.ID, deny)
	p.emit(Event{Kind: EvSystem, Text: why})
}

func (p *codexProc) cachedItem(id string) (codexItem, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	it, ok := p.items[id]
	return it, ok
}

func (p *codexProc) commandApproval(m rpcMsg) {
	var a struct {
		ItemID  string  `json:"itemId"`
		Kind    string  `json:"kind"`
		Command *string `json:"command"`
		Cwd     *string `json:"cwd"`
		Reason  *string `json:"reason"`
		Network *struct {
			Host     string `json:"host"`
			Protocol string `json:"protocol"`
		} `json:"networkApprovalContext"`
		Available []json.RawMessage `json:"availableDecisions"`
	}
	if json.Unmarshal(m.Params, &a) != nil {
		p.refuse(m, verdict("decline"), "Codex sent an unreadable command approval; it was declined")
		return
	}
	command := ""
	if a.Command != nil {
		command = *a.Command
	} else if it, ok := p.cachedItem(a.ItemID); ok {
		command = it.Command
	}
	if command == "" && a.Network == nil {
		p.refuse(m, verdict("decline"), "Codex asked to run a command Orole could not show; it was declined")
		return
	}
	if len(m.Params) > codexPermInputCap {
		p.refuse(m, verdict("decline"), "Codex asked to run a command too large to show; it was declined")
		return
	}
	summary := command
	switch {
	case a.Network != nil:
		summary = fmt.Sprintf("Network access to %s (%s): %s", a.Network.Host, a.Network.Protocol, command)
	case a.Kind == "writeStdin":
		summary = "Send input to a running command: " + command
	}
	if a.Reason != nil && *a.Reason != "" {
		summary += " — " + *a.Reason
	}
	var allow any = verdict("accept")
	if len(a.Available) > 0 {
		allow = nil
		for _, d := range a.Available {
			if string(d) == `"accept"` {
				allow = verdict("accept")
			}
		}
	}
	p.ask(m, "shell", summary, m.Params, allow, verdict("decline"))
}

func (p *codexProc) fileChangeApproval(m rpcMsg) {
	var a struct {
		ItemID    string  `json:"itemId"`
		Reason    *string `json:"reason"`
		GrantRoot *string `json:"grantRoot"`
	}
	if json.Unmarshal(m.Params, &a) != nil {
		p.refuse(m, verdict("decline"), "Codex sent an unreadable file-change approval; it was declined")
		return
	}
	it, ok := p.cachedItem(a.ItemID)
	if !ok || len(it.Changes) == 0 {
		p.refuse(m, verdict("decline"), "Codex asked to change files Orole could not show; it was declined")
		return
	}
	if len(it.Changes) > 100 {
		p.refuse(m, verdict("decline"), fmt.Sprintf("Codex asked to change %d files at once, too many to show; it was declined", len(it.Changes)))
		return
	}
	input, _ := json.Marshal(map[string]any{"reason": a.Reason, "grantRoot": a.GrantRoot, "changes": codexChangeViews(it.Changes, true)})
	if len(input) > codexPermInputCap {
		p.refuse(m, verdict("decline"), "Codex asked to change files with a diff too large to show; it was declined")
		return
	}
	paths := make([]string, 0, len(it.Changes))
	for _, c := range it.Changes {
		paths = append(paths, c.Path)
	}
	summary := fmt.Sprintf("Change %d file(s): %s", len(paths), strings.Join(paths, ", "))
	if a.Reason != nil && *a.Reason != "" {
		summary += " — " + *a.Reason
	}
	p.ask(m, "apply_patch", summary, input, verdict("accept"), verdict("decline"))
}

func (p *codexProc) permissionsApproval(m rpcMsg) {
	var a struct {
		Reason      *string         `json:"reason"`
		Cwd         string          `json:"cwd"`
		Permissions json.RawMessage `json:"permissions"`
	}
	var perms struct {
		FileSystem *struct {
			Read    []string          `json:"read"`
			Write   []string          `json:"write"`
			Entries []json.RawMessage `json:"entries"`
		} `json:"fileSystem"`
		Network *struct {
			Enabled *bool `json:"enabled"`
		} `json:"network"`
	}
	if json.Unmarshal(m.Params, &a) != nil || json.Unmarshal(a.Permissions, &perms) != nil || len(m.Params) > codexPermInputCap {
		p.refuse(m, map[string]any{"permissions": map[string]any{}, "scope": "turn"}, "Codex sent a permission request Orole could not show; it was declined")
		return
	}
	var parts []string
	if fs := perms.FileSystem; fs != nil {
		if len(fs.Read) > 0 {
			parts = append(parts, "read "+strings.Join(fs.Read, ", "))
		}
		if len(fs.Write) > 0 {
			parts = append(parts, "write "+strings.Join(fs.Write, ", "))
		}
		if len(fs.Entries) > 0 {
			parts = append(parts, fmt.Sprintf("%d file-system rule(s), see details", len(fs.Entries)))
		}
	}
	if n := perms.Network; n != nil && n.Enabled != nil {
		parts = append(parts, fmt.Sprintf("network access %v", *n.Enabled))
	}
	if len(parts) == 0 {
		p.refuse(m, map[string]any{"permissions": map[string]any{}, "scope": "turn"}, "Codex asked for permissions Orole could not describe; they were declined")
		return
	}
	summary := "Grant for this turn in " + a.Cwd + ": " + strings.Join(parts, "; ")
	if a.Reason != nil && *a.Reason != "" {
		summary += " — " + *a.Reason
	}
	p.ask(m, "permissions", summary, m.Params,
		map[string]any{"permissions": a.Permissions, "scope": "turn"},
		map[string]any{"permissions": map[string]any{}, "scope": "turn"})
}

func (p *codexProc) legacyExecApproval(m rpcMsg) {
	var a struct {
		Command []string `json:"command"`
		Cwd     string   `json:"cwd"`
		Reason  *string  `json:"reason"`
	}
	if json.Unmarshal(m.Params, &a) != nil || len(a.Command) == 0 || len(m.Params) > codexPermInputCap {
		p.refuse(m, verdict("denied"), "Codex asked to run a command Orole could not show; it was denied")
		return
	}
	summary := strings.Join(a.Command, " ")
	if a.Reason != nil && *a.Reason != "" {
		summary += " — " + *a.Reason
	}
	p.ask(m, "shell", summary, m.Params, verdict("approved"), verdict("denied"))
}

func (p *codexProc) legacyPatchApproval(m rpcMsg) {
	var a struct {
		FileChanges map[string]json.RawMessage `json:"fileChanges"`
		Reason      *string                    `json:"reason"`
	}
	if json.Unmarshal(m.Params, &a) != nil || len(a.FileChanges) == 0 || len(m.Params) > codexPermInputCap {
		p.refuse(m, verdict("denied"), "Codex asked to change files Orole could not show; it was denied")
		return
	}
	paths := make([]string, 0, len(a.FileChanges))
	for path := range a.FileChanges {
		paths = append(paths, path)
	}
	summary := fmt.Sprintf("Change %d file(s): %s", len(paths), strings.Join(paths, ", "))
	if a.Reason != nil && *a.Reason != "" {
		summary += " — " + *a.Reason
	}
	p.ask(m, "apply_patch", summary, m.Params, verdict("approved"), verdict("denied"))
}

// elicitation relays MCP tool-call approvals ("Allow the X MCP server to run tool Y?": a form with
// no fields, a yes/no). Forms that need typed input or a browser URL cannot be answered from an
// approval button and are declined.
func (p *codexProc) elicitation(m rpcMsg) {
	var e struct {
		Server          string `json:"serverName"`
		Mode            string `json:"mode"`
		Message         string `json:"message"`
		RequestedSchema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"requestedSchema"`
	}
	declined := map[string]any{"action": "decline"}
	if json.Unmarshal(m.Params, &e) != nil || len(m.Params) > codexPermInputCap {
		p.refuse(m, declined, "Codex sent an unreadable MCP request; it was declined")
		return
	}
	if e.Mode != "form" || len(e.RequestedSchema.Properties) > 0 || strings.TrimSpace(e.Message) == "" {
		p.refuse(m, declined, fmt.Sprintf("MCP server %q asked for input Orole cannot collect (%s: %s); it was declined", e.Server, e.Mode, truncate(e.Message, 200)))
		return
	}
	p.ask(m, "mcp:"+e.Server, e.Message, m.Params, map[string]any{"action": "accept", "content": nil}, declined)
}

// userInput refuses Codex's clarifying questions: Orole has no way to show answer fields yet, and
// failing the call tells the model to continue on its own judgement.
func (p *codexProc) userInput(m rpcMsg) {
	var a struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(m.Params, &a)
	var qs []string
	for _, q := range a.Questions {
		qs = append(qs, q.Question)
	}
	p.replyError(m.ID, -32000, "The user cannot be asked questions in this session; continue with your best judgement and state your assumptions.")
	p.emit(Event{Kind: EvSystem, Text: "Codex asked a question Orole cannot show yet: " + truncate(strings.Join(qs, " / "), 400)})
}

// requestResolved drops a pending request the server no longer waits for (turn interrupted,
// answered elsewhere).
func (p *codexProc) requestResolved(rpcID json.RawMessage) {
	p.mu.Lock()
	id, ok := p.byRPC[string(rpcID)]
	if ok {
		delete(p.byRPC, string(rpcID))
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if ok {
		p.emit(Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
	}
}

// Respond answers a pending approval. The decision itself was made by the daemon (policy and the
// user's click); a denial carries no reason to Codex because the protocol has no field for one.
func (p *codexProc) Respond(id string, allow bool, _ string) error {
	p.mu.Lock()
	pd, ok := p.pending[id]
	if ok && allow && pd.allow == nil {
		p.mu.Unlock()
		return errors.New("Codex offered no one-time approval for this request; deny it instead")
	}
	if ok {
		delete(p.pending, id)
		delete(p.byRPC, pd.rpcKey)
	}
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending permission request %q", id)
	}
	if allow {
		return p.reply(pd.rpcID, pd.allow)
	}
	return p.reply(pd.rpcID, pd.deny)
}
