// Command fakeagent is a scripted stand-in for the `claude` CLI used by the agents tests. It
// speaks the stream-json protocol (-p mode) including can_use_tool permission requests, and
// a trivial line protocol when started without -p (terminal mode).
//
// Directives found in a prompt:
//
//	NEEDPERM  ask for permission to run a Bash command and report the decision
//	FAIL      finish the turn with an error result
//	SLOW      wait 600ms before answering
//	CRASH     exit with code 3 and a message on stderr
//	EDIT      create edited.txt in the working directory
//	command-output   create ok.txt (what the loop-until fix request triggers)
//	WHICHMODE        answer with the current permission mode and the launch arguments
//
// Like the real CLI it reports --permission-mode in the init message, accepts a
// set_permission_mode control request, and refuses bypassPermissions unless it was launched
// with --allow-dangerously-skip-permissions.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

var out = bufio.NewWriter(os.Stdout)

var (
	permissionMode = "default"
	allowBypass    bool
)

func emit(v any) {
	b, _ := json.Marshal(v)
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
}

func main() {
	var sid, resume string
	print := false
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--version":
			fmt.Println("0.0.0-fake (FakeAgent)")
			return
		case "-p":
			print = true
		case "--session-id":
			i++
			sid = os.Args[i]
		case "--resume":
			i++
			resume = os.Args[i]
			sid = resume
		case "--permission-mode":
			i++
			permissionMode = os.Args[i]
			allowBypass = allowBypass || permissionMode == "bypassPermissions"
		case "--allow-dangerously-skip-permissions":
			allowBypass = true
		}
	}
	if !print {
		terminal()
		return
	}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	started := false
	for {
		line, err := in.ReadString('\n')
		if strings.TrimSpace(line) != "" {
			var m struct {
				Type    string `json:"type"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(line), &m) == nil && m.Type == "user" {
				if !started {
					started = true
					emit(map[string]any{"type": "system", "subtype": "init", "session_id": sid, "model": "fake-1", "permissionMode": permissionMode})
				}
				turn(in, m.Message.Content, sid, resume)
			} else if m.Type == "control_request" {
				setMode(line)
			}
		}
		if err != nil {
			return
		}
	}
}

func setMode(line string) {
	var r struct {
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
			Mode    string `json:"mode"`
		} `json:"request"`
	}
	if json.Unmarshal([]byte(line), &r) != nil || r.Request.Subtype != "set_permission_mode" {
		return
	}
	resp := map[string]any{"subtype": "success", "request_id": r.RequestID, "response": map[string]any{"mode": r.Request.Mode}}
	if r.Request.Mode == "bypassPermissions" && !allowBypass {
		resp = map[string]any{"subtype": "error", "request_id": r.RequestID, "error_code": "bypass_not_launched",
			"error": "Cannot set permission mode to bypassPermissions because the session was not launched with --dangerously-skip-permissions"}
	} else {
		permissionMode = r.Request.Mode
	}
	emit(map[string]any{"type": "control_response", "response": resp})
}

func terminal() {
	fmt.Println("fake terminal ready")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "exit":
			fmt.Println("bye")
			return
		case "crash":
			os.Exit(3)
		default:
			fmt.Println("got:", sc.Text())
		}
	}
}

func text(s string) {
	emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
		"content": []any{map[string]any{"type": "text", "text": s}}}})
}

func result(ok bool, s string) {
	sub := "success"
	if !ok {
		sub = "error_during_execution"
	}
	emit(map[string]any{"type": "result", "subtype": sub, "is_error": !ok, "result": s})
}

func turn(in *bufio.Reader, content, sid, resume string) {
	if strings.Contains(content, "SLOW") {
		time.Sleep(600 * time.Millisecond)
	}
	if strings.Contains(content, "CRASH") {
		fmt.Fprintln(os.Stderr, "fakeagent crashing on purpose")
		os.Exit(3)
	}
	if strings.Contains(content, "EDIT") {
		_ = os.WriteFile("edited.txt", []byte("line one\nline two\n"), 0o644)
	}
	if strings.Contains(content, "command-output") {
		_ = os.WriteFile("ok.txt", []byte("fixed\n"), 0o644)
	}
	emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
		"content": []any{map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{"file_path": "README.md"}}}}})
	if strings.Contains(content, "NEEDPERM") {
		emit(map[string]any{"type": "control_request", "request_id": "req-1", "request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "echo hi"}, "tool_use_id": "tu-1"}})
		for {
			line, err := in.ReadString('\n')
			if strings.TrimSpace(line) != "" {
				var r struct {
					Type     string `json:"type"`
					Response struct {
						Subtype   string `json:"subtype"`
						RequestID string `json:"request_id"`
						Response  struct {
							Behavior     string          `json:"behavior"`
							Message      string          `json:"message"`
							UpdatedInput json.RawMessage `json:"updatedInput"`
						} `json:"response"`
					} `json:"response"`
				}
				if json.Unmarshal([]byte(line), &r) == nil && r.Type == "control_response" && r.Response.RequestID == "req-1" {
					if r.Response.Response.Behavior == "allow" {
						text("ran: echo hi input=" + string(r.Response.Response.UpdatedInput))
						result(true, "ran: echo hi")
					} else {
						text("denied: " + r.Response.Response.Message)
						result(true, "denied")
					}
					return
				}
			}
			if err != nil {
				os.Exit(0)
			}
		}
	}
	if strings.Contains(content, "FAIL") {
		result(false, "boom")
		return
	}
	if strings.Contains(content, "WHICHMODE") {
		msg := fmt.Sprintf("mode=%s args=%s", permissionMode, strings.Join(os.Args[1:], " "))
		text(msg)
		result(true, msg)
		return
	}
	mode := "fresh"
	if resume != "" {
		mode = "resume"
	}
	msg := fmt.Sprintf("echo(%s session=%s): %s", mode, sid, content)
	text(msg)
	result(true, msg)
}
