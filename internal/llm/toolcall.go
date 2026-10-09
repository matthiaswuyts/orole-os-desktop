package llm

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ToolAssembler rebuilds complete client tool calls from streamed fragments.
// Calls are identified by a provider key (a chunk index or an output item id).
// The zero value is ready to use. Not safe for concurrent use.
type ToolAssembler struct {
	calls []*pendingCall
	byKey map[string]*pendingCall
	seq   int
}

type pendingCall struct {
	id, name string
	args     strings.Builder
	done     bool
}

// Begin registers or updates the call under key. A different non-empty id under an
// existing key starts a new call: some gateways reuse index 0 for every call.
func (a *ToolAssembler) Begin(key, id, name string) {
	c := a.byKey[key]
	if c != nil && id != "" && c.id != "" && id != c.id {
		c = nil
	}
	if c == nil {
		c = &pendingCall{}
		if a.byKey == nil {
			a.byKey = map[string]*pendingCall{}
		}
		a.byKey[key] = c
		a.calls = append(a.calls, c)
	}
	if id != "" {
		c.id = id
	}
	if name != "" {
		c.name = name
	}
}

// Append adds an argument fragment to the call under key.
func (a *ToolAssembler) Append(key, fragment string) {
	if c := a.byKey[key]; c != nil {
		c.args.WriteString(fragment)
	}
}

// SetArgs replaces the accumulated arguments (used when the provider sends them whole).
func (a *ToolAssembler) SetArgs(key, args string) {
	if c := a.byKey[key]; c != nil {
		c.args.Reset()
		c.args.WriteString(args)
	}
}

// Take completes the call under key. ok is false when the key is unknown or already taken.
func (a *ToolAssembler) Take(key string) (call ToolCall, ok bool, err error) {
	c := a.byKey[key]
	if c == nil || c.done {
		return ToolCall{}, false, nil
	}
	call, err = a.finish(c)
	return call, true, err
}

// Drain completes every call not yet taken, in order of first appearance.
func (a *ToolAssembler) Drain() ([]ToolCall, error) {
	var out []ToolCall
	for _, c := range a.calls {
		if c.done {
			continue
		}
		call, err := a.finish(c)
		if err != nil {
			return out, err
		}
		out = append(out, call)
	}
	return out, nil
}

// Pending reports whether any call has begun and not been taken.
func (a *ToolAssembler) Pending() bool {
	for _, c := range a.calls {
		if !c.done {
			return true
		}
	}
	return false
}

func (a *ToolAssembler) finish(c *pendingCall) (ToolCall, error) {
	c.done = true
	if c.name == "" {
		return ToolCall{}, &Error{Code: ErrServer, Message: "the model returned a tool call without a name"}
	}
	args := strings.TrimSpace(c.args.String())
	if args == "" {
		args = "{}"
	}
	if !json.Valid([]byte(args)) {
		return ToolCall{}, &Error{Code: ErrServer, Message: "the model returned malformed arguments for tool " + strconv.Quote(c.name)}
	}
	id := c.id
	if id == "" {
		a.seq++
		id = "call_" + strconv.Itoa(a.seq)
	}
	return ToolCall{ID: id, Name: c.name, Args: json.RawMessage(args)}, nil
}
