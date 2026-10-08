package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

const (
	maxResultText  = 256 << 10 // per text block
	maxResultTotal = 1 << 20
	maxArgsDetail  = 1500
)

// ErrCanceled is returned when a call was canceled via CancelCall or its context.
var ErrCanceled = errors.New("mcp: call canceled")

// CallTool invokes a discovered tool WITHOUT a policy check. It is the primitive for
// adapters that apply their own, semantically named policy action (mail.send, ...).
// Anything acting on behalf of a user, agent or automation directly must use InvokeChecked.
func (m *Manager) CallTool(ctx context.Context, id, tool string, args map[string]any, opts CallOptions) (*CallResult, error) {
	c, err := m.conn(id)
	if err != nil {
		return nil, err
	}
	sess, cat := c.live()
	if sess == nil {
		return nil, fmt.Errorf("%w (status %s)", ErrNotConnected, c.statusNow())
	}
	if cat == nil || !catalogHas(cat, tool) {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchTool, untrusted.Line(tool, 80))
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = m.callTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if opts.CallID != "" {
		c.trackCall(opts.CallID, cancel)
		defer c.untrackCall(opts.CallID)
	}
	if err := cctx.Err(); err != nil {
		return nil, err
	}
	if opts.BeforeCall != nil {
		if err := opts.BeforeCall(cctx); err != nil {
			return nil, err
		}
	}
	start := time.Now()
	res, err := sess.CallTool(cctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	lat := time.Since(start)
	c.noteLatency(lat)
	if m.events != nil {
		m.publishToolEvent(ctx, id, tool, err == nil && res != nil && !res.IsError)
	}
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			return nil, fmt.Errorf("tool call timed out after %s", timeout)
		case errors.Is(err, context.Canceled):
			return nil, ErrCanceled
		case errors.Is(err, ErrReauthRequired):
			c.markAuthRequired(err)
			return nil, fmt.Errorf("%w: %s", ErrReauthRequired, untrusted.Line(err.Error(), 300))
		}
		return nil, fmt.Errorf("tool call failed: %s", untrusted.Line(err.Error(), 400))
	}
	out := convertResult(res)
	out.LatencyMs = lat.Milliseconds()
	return out, nil
}

func catalogHas(c *Catalog, tool string) bool {
	for _, t := range c.Tools {
		if t.Name == tool {
			return true
		}
	}
	return false
}

// InvokeChecked invokes a tool after consulting the policy engine; side-effecting
// tools block on a user approval. The tool's own annotations only pick the effect
// class (hint), they never grant permission.
func (m *Manager) InvokeChecked(ctx context.Context, id, tool string, args map[string]any, opts CallOptions) (*CallResult, error) {
	if m.policy == nil {
		return nil, errors.New("mcp: no policy engine configured; refusing to call tools unchecked")
	}
	if opts.CallID != "" {
		c, err := m.conn(id)
		if err != nil {
			return nil, err
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		c.trackCall(opts.CallID, cancel)
		defer c.untrackCall(opts.CallID)
		opts.CallID = "" // one cancellation registration covers approval and invocation
	}
	cat, err := m.Catalog(ctx, id)
	if err != nil {
		return nil, err
	}
	var ti *ToolInfo
	for i := range cat.Tools {
		if cat.Tools[i].Name == tool {
			ti = &cat.Tools[i]
		}
	}
	if ti == nil {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchTool, untrusted.Line(tool, 80))
	}
	actor := opts.Actor
	if actor == "" {
		actor = "user"
	}
	cfg, err := m.server(ctx, id)
	if err != nil {
		return nil, err
	}
	aj, _ := json.Marshal(args)
	detail := fmt.Sprintf("Server %q wants to run tool %q.\nArguments: %s", cfg.Name, tool, untrusted.Line(string(aj), maxArgsDetail))
	err = m.policy.Require(ctx, policy.Request{Actor: actor, Action: "mcp.tool.call", Resource: "mcp/" + id + "/" + tool,
		Effect: policy.Effect(ti.Effect), FromHint: true, Detail: detail})
	if err != nil {
		return nil, err
	}
	return m.CallTool(ctx, id, tool, args, opts)
}

// CancelCall cancels an in-flight call started with the given CallID.
func (m *Manager) CancelCall(callID string) bool {
	m.mu.Lock()
	conns := make([]*conn, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		if c.cancelCall(callID) {
			return true
		}
	}
	return false
}

// convertResult flattens an SDK result into sanitised, size-bounded blocks. Binary
// payloads are summarised, not forwarded.
func convertResult(res *sdk.CallToolResult) *CallResult {
	out := &CallResult{IsError: res.IsError, Untrusted: true, Content: []ContentBlock{}}
	raw, err := json.Marshal(res)
	if err != nil {
		return out
	}
	var w struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
			URI      string `json:"uri"`
			Resource *struct {
				URI      string `json:"uri"`
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
				Blob     string `json:"blob"`
			} `json:"resource"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &w) != nil {
		return out
	}
	total := 0
	text := func(s string) (string, bool) {
		trunc := false
		if len(s) > maxResultText {
			s, trunc = s[:maxResultText], true
		}
		if total+len(s) > maxResultTotal {
			s, trunc = s[:max(0, maxResultTotal-total)], true
		}
		total += len(s)
		return untrusted.Text(s, 0), trunc
	}
	for _, c := range w.Content {
		b := ContentBlock{Type: untrusted.Line(c.Type, 24), MimeType: untrusted.Line(c.MimeType, 120), URI: untrusted.Line(c.URI, 1024)}
		switch c.Type {
		case "text":
			b.Text, b.Truncated = text(c.Text)
		case "image", "audio":
			b.Bytes = base64Len(c.Data)
		case "resource":
			if c.Resource != nil {
				b.URI = untrusted.Line(c.Resource.URI, 1024)
				b.MimeType = untrusted.Line(c.Resource.MimeType, 120)
				if c.Resource.Text != "" {
					b.Text, b.Truncated = text(c.Resource.Text)
				}
				b.Bytes = base64Len(c.Resource.Blob)
			}
		}
		out.Content = append(out.Content, b)
	}
	if len(w.Structured) > 0 && len(w.Structured) <= maxResultTotal && string(w.Structured) != "null" {
		out.Structured = untrusted.JSON(w.Structured, maxResultTotal)
	}
	return out
}

func base64Len(s string) int { return len(s) / 4 * 3 }
