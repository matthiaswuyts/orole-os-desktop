package agents

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type permissionSink struct{ bytes.Buffer }

func (*permissionSink) Close() error { return nil }

func permissionFixture() (*pipeProc, *permissionSink) {
	sink := &permissionSink{}
	return &pipeProc{stdin: sink, exited: make(chan struct{}), pendingIn: make(map[string]pendingPermission), encodeReply: encodeClaudeReply}, sink
}

func decodeClaude(p *pipeProc, line []byte) []Event {
	return (&claudeDecoder{tools: make(map[string]ToolData)}).decode(p, line)
}

func permissionMessage(id, input string) []byte {
	return []byte(`{"type":"control_request","request_id":` + string(jraw(id)) + `,"request":{"subtype":"can_use_tool","tool_name":"Bash","input":` + input + `}}`)
}

func decodedPermission(t *testing.T, events []Event) PermissionRequest {
	t.Helper()
	if len(events) != 1 || events[0].Kind != EvPermission {
		t.Fatalf("permission events: %+v", events)
	}
	var request PermissionRequest
	if err := json.Unmarshal(events[0].Data, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestPermissionSnapshotDisclosesFullArguments(t *testing.T) {
	p, sink := permissionFixture()
	input := `{"command":` + string(jraw("echo "+strings.Repeat("padding ", 80)+"; delete-sensitive-target")) + `,"other":{"replacement":"<script>untrusted</script>","large_integer":9007199254740993}}`
	request := decodedPermission(t, decodeClaude(p, permissionMessage("wire-1", input)))
	if request.InputJSON != input {
		t.Fatal("complete approval arguments were altered")
	}
	if err := p.Respond(request.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior string          `json:"behavior"`
				Input    json.RawMessage `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(sink.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, reply.Response.Response.Input); err != nil {
		t.Fatal(err)
	}
	if reply.Response.RequestID != "wire-1" || reply.Response.Response.Behavior != "allow" || !strings.Contains(compact.String(), "9007199254740993") || !strings.Contains(compact.String(), "delete-sensitive-target") {
		t.Fatalf("wrong dispatched snapshot: %s", sink.Bytes())
	}
}

func TestPermissionIDsCannotAuthorizeReplacement(t *testing.T) {
	p, sink := permissionFixture()
	first := decodedPermission(t, decodeClaude(p, permissionMessage("wire-1", `{"command":"echo first"}`)))
	canceled := decodeClaude(p, []byte(`{"type":"control_cancel_request","request_id":"wire-1"}`))
	if len(canceled) != 1 || canceled[0].Kind != EvPermCancel {
		t.Fatalf("cancellation: %+v", canceled)
	}
	second := decodedPermission(t, decodeClaude(p, permissionMessage("wire-1", `{"command":"echo second"}`)))
	if err := p.Respond(first.ID, true, ""); err == nil {
		t.Fatal("stale approval accepted")
	}
	if sink.Len() != 0 {
		t.Fatal("stale approval reached agent")
	}
	if err := p.Respond(second.ID, false, "denied replacement"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.String(), `"behavior":"deny"`) || strings.Contains(sink.String(), "echo second") {
		t.Fatalf("replacement not denied: %s", sink.Bytes())
	}
}

func TestPermissionDuplicateAndOversizedFailClosed(t *testing.T) {
	for _, kind := range []string{"duplicate", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			p, sink := permissionFixture()
			input := `{"command":"echo safe"}`
			var first PermissionRequest
			if kind == "duplicate" {
				first = decodedPermission(t, decodeClaude(p, permissionMessage("wire-1", input)))
			} else {
				input = `{"command":` + string(jraw(strings.Repeat("x", 64<<10))) + `}`
			}
			events := decodeClaude(p, permissionMessage("wire-1", input))
			for _, event := range events {
				if event.Kind == EvPermission {
					t.Fatal("unsafe request exposed for approval")
				}
			}
			if len(p.pendingIn) != 0 || !strings.Contains(sink.String(), `"behavior":"deny"`) {
				t.Fatalf("request not denied: %s", sink.Bytes())
			}
			if kind == "duplicate" && p.Respond(first.ID, true, "") == nil {
				t.Fatal("duplicate retained stale approval")
			}
		})
	}
}
