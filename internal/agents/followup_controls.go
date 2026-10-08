package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type controlReply struct {
	data json.RawMessage
	err  error
}

// controlCall waits for the harness acknowledgement, not merely a successful
// stdin write. The decoder routes replies independently of manager event folding.
func (p *pipeProc) controlCall(request map[string]any, claude bool, out any) error {
	id := "orole-config-" + uuid.NewString()
	ch := make(chan controlReply, 1)
	p.controlMu.Lock()
	if p.controlPending == nil {
		p.controlPending = make(map[string]chan controlReply)
	}
	p.controlPending[id] = ch
	p.controlMu.Unlock()
	defer func() {
		p.controlMu.Lock()
		delete(p.controlPending, id)
		p.controlMu.Unlock()
	}()
	var frame map[string]any
	if claude {
		frame = map[string]any{"type": "control_request", "request_id": id, "request": request}
	} else {
		frame = request
		frame["id"] = id
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if err := p.write(append(b, '\n')); err != nil {
		return err
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case reply := <-ch:
		if reply.err != nil {
			return reply.err
		}
		if out != nil {
			return json.Unmarshal(reply.data, out)
		}
		return nil
	case <-p.exited:
		return errors.New("agent exited before acknowledging its configuration")
	case <-timer.C:
		return errors.New("agent configuration acknowledgement timed out; configuration may have changed")
	}
}

func (p *pipeProc) acceptControlResponse(line []byte, claude bool) bool {
	var r struct {
		Type     string          `json:"type"`
		ID       string          `json:"id"`
		Success  bool            `json:"success"`
		Error    string          `json:"error"`
		Data     json.RawMessage `json:"data"`
		Response struct {
			ID      string `json:"request_id"`
			Subtype string `json:"subtype"`
			Error   string `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	if claude {
		if r.Type != "control_response" {
			return false
		}
		r.ID, r.Success, r.Error = r.Response.ID, r.Response.Subtype == "success", r.Response.Error
	} else if r.Type != "response" {
		return false
	}
	if !strings.HasPrefix(r.ID, "orole-config-") {
		return false
	}
	p.controlMu.Lock()
	ch := p.controlPending[r.ID]
	delete(p.controlPending, r.ID)
	p.controlMu.Unlock()
	if ch != nil {
		var err error
		if !r.Success {
			err = fmt.Errorf("agent rejected configuration: %s", r.Error)
		}
		ch <- controlReply{data: r.Data, err: err}
	}
	return true
}

func (*claudeProc) FollowupOptions() (bool, bool) { return true, false }

// set_model is the official Agent SDK control request. Effort is a CLI launch
// option, not set_max_thinking_tokens (a different control), so it is not faked.
// https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/_internal/query.py
func (p *claudeProc) SendConfigured(prompt, model, reasoning string) error {
	if reasoning != "" {
		return errors.New("Claude effort cannot be changed on a running conversation")
	}
	if model != "" {
		if err := p.controlCall(map[string]any{"subtype": "set_model", "model": model}, true, nil); err != nil {
			return err
		}
	}
	return p.Send(prompt)
}

func (*ompProc) FollowupOptions() (bool, bool) { return true, true }

// These commands and their responses are defined by OMP's installed
// src/modes/rpc/rpc-types.ts and handled by rpc-mode.ts on the existing session.
func (p *ompProc) SendConfigured(prompt, model, reasoning string) error {
	if model != "" {
		provider, name, ok := strings.Cut(model, "/")
		if !ok || provider == "" || name == "" {
			return errors.New("Oh My Pi requires a provider/model selector")
		}
		if err := p.controlCall(map[string]any{"type": "set_model", "provider": provider, "modelId": name}, false, nil); err != nil {
			return err
		}
	}
	if reasoning != "" {
		if err := p.controlCall(map[string]any{"type": "set_thinking_level", "level": reasoning}, false, nil); err != nil {
			return err
		}
	}
	// OMP clamps effort to model metadata and may choose a model's default on
	// set_model. Read the actual state so an acknowledged clamp is not reported
	// as having applied a setting that the provider did not accept.
	var state struct {
		Model struct {
			Provider string `json:"provider"`
			ID       string `json:"id"`
		} `json:"model"`
		ThinkingLevel string `json:"thinkingLevel"`
	}
	if err := p.controlCall(map[string]any{"type": "get_state"}, false, &state); err != nil {
		return err
	}
	if model != "" && state.Model.Provider+"/"+state.Model.ID != model {
		return errors.New("Oh My Pi did not apply the requested model; follow-up was not sent")
	}
	if reasoning != "" && state.ThinkingLevel != reasoning {
		return fmt.Errorf("Oh My Pi applied effort %q instead of %q; follow-up was not sent", state.ThinkingLevel, reasoning)
	}
	return p.Send(prompt)
}
