// Command fakecodex is a scripted stand-in for `codex app-server` used by the agents tests. It
// speaks the JSON-RPC lines protocol (initialize, thread/start|resume, turn/start|steer|interrupt,
// model/list) and replays notifications and approval requests chosen by directives in the prompt:
//
//	TOOLS     reasoning, command approval, file-change approval, image, MCP call, final message
//	DENYME    like TOOLS but stops after the first approval and reports the decision it received
//	FAIL      finish the turn as failed
//	SLOW      keep the turn open until it is steered or interrupted
//	ELICIT    MCP tool-call approval (form without fields), then a form that needs input
//	ASK       item/tool/requestUserInput
//	BIGIMAGE  imageGeneration with a payload that is not an image
//	CRASH     exit with code 3 and a message on stderr
//
// FAKECODEX_LOG names a file receiving one JSON line per client request (method + params).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	out    = bufio.NewWriter(os.Stdout)
	outMu  sync.Mutex
	logMu  sync.Mutex
	logf   *os.File
	nextID = 100
	idMu   sync.Mutex

	waitMu  sync.Mutex
	waiters = map[string]chan json.RawMessage{}

	turnMu     sync.Mutex
	turnN      int
	activeTurn string
	steerCh    = make(chan string, 4)
	intrCh     = make(chan struct{}, 1)
)

// onePixelPNG is a valid 1x1 PNG.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func send(v any) {
	b, _ := json.Marshal(v)
	outMu.Lock()
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
	outMu.Unlock()
}

func notify(method string, params any) { send(map[string]any{"method": method, "params": params}) }

func logReq(method string, params json.RawMessage) {
	if logf == nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	b, _ := json.Marshal(map[string]any{"method": method, "params": params})
	logf.Write(append(b, '\n'))
}

// serverRequest sends a request to the client and waits for its result.
func serverRequest(method string, params any) json.RawMessage {
	idMu.Lock()
	nextID++
	id := fmt.Sprint(nextID)
	idMu.Unlock()
	ch := make(chan json.RawMessage, 1)
	waitMu.Lock()
	waiters[id] = ch
	waitMu.Unlock()
	send(map[string]any{"id": json.Number(id), "method": method, "params": params})
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		return nil
	}
}

func item(typ, id string, extra map[string]any) map[string]any {
	m := map[string]any{"type": typ, "id": id}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func started(thread, turn string, it map[string]any) {
	notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": it, "startedAtMs": 1})
}

func completed(thread, turn string, it map[string]any) {
	notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": it, "completedAtMs": 2})
}

func finish(thread, turn, status string, errMsg string) {
	t := map[string]any{"id": turn, "status": status, "items": []any{}, "error": nil}
	if errMsg != "" {
		t["error"] = map[string]any{"message": errMsg}
	}
	turnMu.Lock()
	activeTurn = ""
	turnMu.Unlock()
	notify("turn/completed", map[string]any{"threadId": thread, "turn": t})
}

func message(thread, turn, id, phase, text string) {
	started(thread, turn, item("agentMessage", id, map[string]any{"text": "", "phase": phase}))
	notify("item/agentMessage/delta", map[string]any{"threadId": thread, "turnId": turn, "itemId": id, "delta": text})
	completed(thread, turn, item("agentMessage", id, map[string]any{"text": text, "phase": phase}))
}

func decisionOf(r json.RawMessage) string {
	var d struct {
		Decision json.RawMessage `json:"decision"`
		Action   string          `json:"action"`
	}
	_ = json.Unmarshal(r, &d)
	if d.Action != "" {
		return d.Action
	}
	return strings.Trim(string(d.Decision), `"`)
}

func runTurn(thread, turn, text string) {
	notify("turn/started", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "inProgress", "items": []any{}}})
	started(thread, turn, item("userMessage", "um-"+turn, map[string]any{"content": []any{}}))
	completed(thread, turn, item("userMessage", "um-"+turn, map[string]any{"content": []any{}}))
	switch {
	case strings.Contains(text, "FAIL"):
		notify("error", map[string]any{"threadId": thread, "turnId": turn, "willRetry": false,
			"error": map[string]any{"message": `{"type":"error","error":{"message":"model exploded"}}`}})
		finish(thread, turn, "failed", `{"type":"error","error":{"message":"model exploded"}}`)
	case strings.Contains(text, "SLOW"):
		select {
		case s := <-steerCh:
			message(thread, turn, "msg-steer", "final_answer", "steered: "+s)
			finish(thread, turn, "completed", "")
		case <-intrCh:
			finish(thread, turn, "interrupted", "")
		case <-time.After(15 * time.Second):
			finish(thread, turn, "failed", "fake timeout")
		}
	case strings.Contains(text, "ELICIT"):
		r := serverRequest("mcpServer/elicitation/request", map[string]any{
			"threadId": thread, "turnId": turn, "serverName": "orole", "mode": "form",
			"message":         `Allow the orole MCP server to run tool "ping"?`,
			"_meta":           map[string]any{"codex_approval_kind": "mcp_tool_call", "tool_params": map[string]any{"x": 1}},
			"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}}})
		message(thread, turn, "msg-1", "commentary", "elicitation: "+decisionOf(r))
		r = serverRequest("mcpServer/elicitation/request", map[string]any{
			"threadId": thread, "turnId": turn, "serverName": "other", "mode": "form", "message": "Your name?",
			"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}})
		message(thread, turn, "msg-2", "final_answer", "form: "+decisionOf(r))
		finish(thread, turn, "completed", "")
	case strings.Contains(text, "ASK"):
		serverRequest("item/tool/requestUserInput", map[string]any{"threadId": thread, "turnId": turn, "itemId": "q1", "isBlocking": true,
			"questions": []any{map[string]any{"id": "a", "header": "h", "question": "Which database?"}}})
		message(thread, turn, "msg-1", "final_answer", "asked")
		finish(thread, turn, "completed", "")
	case strings.Contains(text, "BIGIMAGE"):
		completed(thread, turn, item("imageGeneration", "img-bad", map[string]any{"status": "completed", "result": "bm90IGFuIGltYWdl", "revisedPrompt": nil}))
		finish(thread, turn, "completed", "")
	case strings.Contains(text, "TOOLS") || strings.Contains(text, "DENYME"):
		started(thread, turn, item("reasoning", "rs-1", map[string]any{"summary": []any{}, "content": []any{}}))
		completed(thread, turn, item("reasoning", "rs-1", map[string]any{"summary": []any{"**Planning the edit**"}, "content": []any{"raw thought"}}))
		cmd := item("commandExecution", "cmd-1", map[string]any{"command": "git status", "cwd": "C:\\work", "status": "inProgress", "commandActions": []any{}})
		started(thread, turn, cmd)
		r := serverRequest("item/commandExecution/requestApproval", map[string]any{"threadId": thread, "turnId": turn, "itemId": "cmd-1",
			"kind": "command", "command": "git status", "cwd": "C:\\work", "reason": "needs to inspect the tree",
			"availableDecisions": []any{"accept", "cancel"}})
		d := decisionOf(r)
		if strings.Contains(text, "DENYME") {
			cmd["status"] = "declined"
			completed(thread, turn, cmd)
			message(thread, turn, "msg-1", "final_answer", "command decision: "+d)
			finish(thread, turn, "completed", "")
			return
		}
		cmd["status"], cmd["exitCode"], cmd["aggregatedOutput"] = "completed", 0, "On branch main\n"
		completed(thread, turn, cmd)
		fc := item("fileChange", "fc-1", map[string]any{"status": "inProgress", "changes": []any{
			map[string]any{"path": "C:\\work\\a.txt", "kind": map[string]any{"type": "add"}, "diff": "hi\n"},
			map[string]any{"path": "C:\\work\\b.txt", "kind": map[string]any{"type": "update", "move_path": nil}, "diff": "@@\n-a\n+b\n"}}})
		started(thread, turn, fc)
		r = serverRequest("item/fileChange/requestApproval", map[string]any{"threadId": thread, "turnId": turn, "itemId": "fc-1", "reason": "write files"})
		fd := decisionOf(r)
		fc["status"] = "completed"
		completed(thread, turn, fc)
		completed(thread, turn, item("imageGeneration", "exec-img-1", map[string]any{"status": "completed", "result": onePixelPNG, "revisedPrompt": "a red dot", "failure": nil}))
		started(thread, turn, item("mcpToolCall", "mcp-1", map[string]any{"server": "orole", "tool": "ping", "status": "inProgress", "arguments": map[string]any{"x": "1"}}))
		completed(thread, turn, item("mcpToolCall", "mcp-1", map[string]any{"server": "orole", "tool": "ping", "status": "completed", "arguments": map[string]any{"x": "1"},
			"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "pong"}}}}))
		message(thread, turn, "msg-1", "final_answer", "command="+d+" files="+fd)
		finish(thread, turn, "completed", "")
	default:
		started(thread, turn, item("reasoning", "rs-1", map[string]any{"summary": []any{}, "content": []any{}}))
		completed(thread, turn, item("reasoning", "rs-1", map[string]any{"summary": []any{"thinking about " + text}, "content": []any{}}))
		message(thread, turn, "msg-1", "final_answer", "echo: "+text)
		finish(thread, turn, "completed", "")
	}
}

func inputText(params json.RawMessage) string {
	var p struct {
		Input []struct {
			Text string `json:"text"`
		} `json:"input"`
	}
	_ = json.Unmarshal(params, &p)
	var parts []string
	for _, i := range p.Input {
		parts = append(parts, i.Text)
	}
	return strings.Join(parts, "\n")
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "app-server" {
		fmt.Fprintln(os.Stderr, "fakecodex: expected `app-server`")
		os.Exit(2)
	}
	if name := os.Getenv("FAKECODEX_LOG"); name != "" {
		logf, _ = os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	for {
		line, err := in.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var m struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Result json.RawMessage `json:"result"`
			}
			if json.Unmarshal(line, &m) == nil {
				handle(m.ID, m.Method, m.Params, m.Result)
			}
		}
		if err != nil {
			return
		}
	}
}

func reply(id json.RawMessage, result any) { send(map[string]any{"id": id, "result": result}) }

func handle(id json.RawMessage, method string, params, result json.RawMessage) {
	if method == "" { // a response to one of our server requests
		waitMu.Lock()
		ch := waiters[string(id)]
		delete(waiters, string(id))
		waitMu.Unlock()
		if ch != nil {
			ch <- result
		}
		return
	}
	logReq(method, params)
	switch method {
	case "initialize":
		reply(id, map[string]any{"userAgent": "fakecodex/1", "codexHome": "C:\\fake", "platformFamily": "windows", "platformOs": "windows"})
	case "initialized":
	case "model/list":
		reply(id, map[string]any{"data": []any{
			map[string]any{"id": "fake-a", "model": "fake-a", "displayName": "Fake A", "hidden": false, "isDefault": true,
				"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "low"}, map[string]any{"reasoningEffort": "high"}}},
			map[string]any{"id": "fake-hidden", "model": "fake-hidden", "displayName": "Hidden", "hidden": true, "supportedReasoningEfforts": []any{}},
			map[string]any{"id": "fake-b", "model": "fake-b", "displayName": "Fake B", "hidden": false,
				"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium"}, map[string]any{"reasoningEffort": "ultra"}}},
		}, "nextCursor": nil})
	case "thread/start":
		reply(id, map[string]any{"thread": map[string]any{"id": "thr-new"}, "model": "fake-model"})
	case "thread/resume":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(params, &p)
		if p.ThreadID != "thr-new" {
			send(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": "no rollout found for thread id " + p.ThreadID}})
			return
		}
		reply(id, map[string]any{"thread": map[string]any{"id": p.ThreadID}, "model": "fake-model"})
	case "turn/start":
		text := inputText(params)
		if strings.Contains(text, "CRASH") {
			fmt.Fprintln(os.Stderr, "fakecodex: crashing on request")
			os.Exit(3)
		}
		turnMu.Lock()
		turnN++
		turn := fmt.Sprintf("turn-%d", turnN)
		activeTurn = turn
		turnMu.Unlock()
		reply(id, map[string]any{"turn": map[string]any{"id": turn, "status": "inProgress", "items": []any{}}})
		go runTurn("thr-new", turn, text)
	case "turn/steer":
		var p struct {
			ExpectedTurnID string `json:"expectedTurnId"`
		}
		_ = json.Unmarshal(params, &p)
		turnMu.Lock()
		cur := activeTurn
		turnMu.Unlock()
		if cur == "" || cur != p.ExpectedTurnID {
			send(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": "no active turn to steer"}})
			return
		}
		reply(id, map[string]any{"turnId": cur})
		steerCh <- inputText(params)
	case "turn/interrupt":
		reply(id, map[string]any{})
		select {
		case intrCh <- struct{}{}:
		default:
		}
	default:
		if len(id) > 0 {
			send(map[string]any{"id": id, "error": map[string]any{"code": -32601, "message": "unknown method " + method}})
		}
	}
}
