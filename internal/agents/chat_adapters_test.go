package agents

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Fixtures follow codex-cli 0.156.1's generated app-server JSON schemas,
// OMP 18.8.4's installed modes/rpc/rpc-types.ts and rpc-mode.ts, and the
// Claude Agent SDK user-input, cost-tracking, and TypeScript message reference.
// They exercise wire semantics without starting a model or an installed CLI.
func chatEventData[T any](t *testing.T, events []Event, kind string) T {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind {
			var value T
			if err := json.Unmarshal(event.Data, &value); err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatalf("missing %s in %+v", kind, events)
	var zero T
	return zero
}

func chatHasEvent(events []Event, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func chatDrain(p *codexProc) []Event {
	var events []Event
	for {
		select {
		case event := <-p.events:
			events = append(events, event)
		default:
			return events
		}
	}
}

func chatCodexFixture() (*codexProc, *permissionSink) {
	sink := &permissionSink{}
	return &codexProc{stdin: sink, exited: make(chan struct{}), events: make(chan Event, 64), threadID: "thread",
		pending: make(map[string]*codexPending), byRPC: make(map[string]string), items: make(map[string]codexItem), live: make(map[string]string)}, sink
}

func chatCodexQuestion(p *codexProc) {
	p.serverRequest(rpcMsg{ID: json.RawMessage(`23`), Method: "item/tool/requestUserInput", Params: json.RawMessage(`{
		"threadId":"thread","turnId":"turn","itemId":"call","isBlocking":true,
		"questions":[{"id":"database","header":"Database","question":"Which database?","isOther":false,"isSecret":false,
		"options":[{"label":"Postgres","description":"Server"},{"label":"SQLite","description":"Local"}]}]}`)})
}

func TestChatCodexUsageUsesLastNotLifetimeTotals(t *testing.T) {
	p, _ := chatCodexFixture()
	p.notification("thread/tokenUsage/updated", json.RawMessage(`{"threadId":"thread","turnId":"turn","tokenUsage":{
		"total":{"inputTokens":900000,"outputTokens":60000,"cachedInputTokens":800000,"reasoningOutputTokens":1000,"totalTokens":960000},
		"last":{"inputTokens":1000,"outputTokens":20,"cachedInputTokens":800,"reasoningOutputTokens":5,"totalTokens":1020},"modelContextWindow":8000}}`))
	u := chatEventData[ContextUsage](t, chatDrain(p), EvUsage)
	if u.ContextTokens == nil || *u.ContextTokens != 1020 || u.ContextWindow == nil || *u.ContextWindow != 8000 || u.At.IsZero() {
		t.Fatalf("wrong occupancy: %+v", u)
	}
	p.notification("thread/tokenUsage/updated", json.RawMessage(`{"threadId":"other","tokenUsage":{"last":{"totalTokens":9}}}`))
	if len(chatDrain(p)) != 0 {
		t.Fatal("foreign usage leaked")
	}
	p.notification("thread/tokenUsage/updated", json.RawMessage(`{"threadId":"thread","tokenUsage":{"last":{"inputTokens":0},"modelContextWindow":null}}`))
	u = chatEventData[ContextUsage](t, chatDrain(p), EvUsage)
	if u.InputTokens == nil || *u.InputTokens != 0 || u.ContextTokens != nil || u.ContextWindow != nil {
		t.Fatalf("absent fields were invented: %+v", u)
	}
}

func TestChatCodexInputNativeRoundTripAndValidation(t *testing.T) {
	p, sink := chatCodexFixture()
	chatCodexQuestion(p)
	events := chatDrain(p)
	req := chatEventData[QuestionRequest](t, events, EvQuestion)
	if chatHasEvent(events, EvPermission) || req.ID == "23" || req.Questions[0].AllowText {
		t.Fatalf("input became approval or mutable wire id: %+v", req)
	}
	if err := p.Respond(req.ID, true, ""); err == nil {
		t.Fatal("permission response accepted a question")
	}
	bad := []QuestionAnswer{{QuestionID: "database", Values: []string{"arbitrary"}}}
	if err := p.AnswerInput(req.ID, bad, false); err == nil || sink.Len() != 0 {
		t.Fatal("unoffered choice reached the protocol")
	}
	// Mutating the published view cannot authorize a different value.
	req.Questions[0].Options[0].ID = "arbitrary"
	if err := p.AnswerInput(req.ID, bad, false); err == nil {
		t.Fatal("published request mutated the pending snapshot")
	}
	if err := p.AnswerInput(req.ID, []QuestionAnswer{{QuestionID: "database", Values: []string{"SQLite"}}}, false); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		ID     int `json:"id"`
		Result struct {
			Answers map[string]struct {
				Answers []string `json:"answers"`
			} `json:"answers"`
		} `json:"result"`
	}
	if err := json.Unmarshal(sink.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ID != 23 || len(wire.Result.Answers["database"].Answers) != 1 || wire.Result.Answers["database"].Answers[0] != "SQLite" {
		t.Fatalf("native response: %s", sink.Bytes())
	}
	if err := p.AnswerInput(req.ID, nil, true); err == nil {
		t.Fatal("consumed question accepted another answer")
	}
}

func TestChatCodexInputWithdrawalAndCancellation(t *testing.T) {
	for _, mode := range []string{"resolved", "completed", "duplicate", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			p, sink := chatCodexFixture()
			chatCodexQuestion(p)
			req := chatEventData[QuestionRequest](t, chatDrain(p), EvQuestion)
			switch mode {
			case "resolved":
				p.notification("serverRequest/resolved", json.RawMessage(`{"threadId":"thread","requestId":23}`))
			case "completed":
				p.turnCompleted("turn", "completed", "")
			case "duplicate":
				p.serverRequest(rpcMsg{ID: json.RawMessage(`23`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"command":"danger"}`)})
			case "cancel":
				if err := p.AnswerInput(req.ID, nil, true); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(sink.String(), `"answers":{}`) || strings.Contains(sink.String(), "decision") {
					t.Fatalf("cancel must be no answers, not permission: %s", sink.String())
				}
			}
			events := chatDrain(p)
			if mode != "cancel" && !chatHasEvent(events, EvQuestionCancel) {
				t.Fatalf("missing withdrawal: %+v", events)
			}
			if chatHasEvent(events, EvPermission) || p.AnswerInput(req.ID, nil, true) == nil || len(p.inputs) != 0 {
				t.Fatal("stale question remained actionable")
			}
		})
	}
}

func TestChatCodexOnlyStructuredArtifactsAndPolicy(t *testing.T) {
	p, _ := chatCodexFixture()
	p.notification("item/completed", json.RawMessage(`{"threadId":"thread","item":{"type":"collabAgentToolCall","id":"spawn","tool":"spawnAgent","status":"completed","receiverThreadIds":["child"],"agentsStates":{"child":{"status":"running","message":"Inspecting"}},"prompt":"Review"}}`))
	agent := chatEventData[SubagentData](t, chatDrain(p), EvSubagent)
	if agent.ID != "child" || agent.Status != "running" || agent.ToolCallID != "spawn" {
		t.Fatalf("tool completion must not finish child: %+v", agent)
	}
	p.notification("item/completed", json.RawMessage(`{"threadId":"thread","item":{"type":"collabAgentToolCall","id":"unknown","tool":"spawnAgent","status":"completed","receiverThreadIds":["unknown"],"agentsStates":{}}}`))
	if chatHasEvent(chatDrain(p), EvSubagent) {
		t.Fatal("receiver id alone invented child state")
	}
	p.notification("item/completed", json.RawMessage(`{"threadId":"thread","item":{"type":"fileChange","id":"patch","status":"declined","changes":[{"path":"src/main.go","kind":{"type":"update"},"diff":"diff"}]}}`))
	file := chatEventData[FileData](t, chatDrain(p), EvFile)
	if file.Path != "src/main.go" || file.Status != "declined" {
		t.Fatalf("file proposal misrepresented: %+v", file)
	}
	p.notification("item/started", json.RawMessage(`{"threadId":"thread","item":{"type":"imageGeneration","id":"image","status":"inProgress","result":""}}`))
	image := chatEventData[ImageStatusData](t, chatDrain(p), EvImageStatus)
	if image.ID != "image" || image.Status != "running" {
		t.Fatalf("image start: %+v", image)
	}
	p.notification("item/completed", json.RawMessage(`{"threadId":"thread","item":{"type":"imageGeneration","id":"image","status":"failed","result":"","failure":{"type":"usageLimitExceeded"}}}`))
	image = chatEventData[ImageStatusData](t, chatDrain(p), EvImageStatus)
	if image.ID != "image" || image.Status != "failed" || image.Error == "" {
		t.Fatalf("image failure: %+v", image)
	}
	p.notification("error", json.RawMessage(`{"threadId":"thread","turnId":"turn","willRetry":false,"error":{"message":"refusal-like prose","codexErrorInfo":"other"}}`))
	if chatHasEvent(chatDrain(p), EvGuardrail) {
		t.Fatal("generic error was invented as policy")
	}
	p.notification("error", json.RawMessage(`{"threadId":"thread","turnId":"turn","willRetry":false,"error":{"message":"blocked","codexErrorInfo":"cyberPolicy"}}`))
	if notice := chatEventData[GuardrailData](t, chatDrain(p), EvGuardrail); notice.Kind != "cyberPolicy" {
		t.Fatalf("policy signal: %+v", notice)
	}
	p.notification("item/completed", json.RawMessage(`{"threadId":"thread","item":{"type":"agentMessage","id":"msg","text":"https://not-a-citation.example","phase":"final_answer"}}`))
	if chatHasEvent(chatDrain(p), EvSources) {
		t.Fatal("prose URL became a citation")
	}
	p.notification("item/completed", json.RawMessage(`{"threadId":"thread","item":{"type":"webSearch","id":"search","query":"","action":{"type":"openPage","url":"https://example.com/source"}}}`))
	if sources := chatEventData[SourcesData](t, chatDrain(p), EvSources); len(sources.Items) != 1 || sources.Items[0].URL != "https://example.com/source" {
		t.Fatalf("reported source: %+v", sources)
	}
}

func TestChatOMPDialogsNativeValuesAndExpiration(t *testing.T) {
	for _, method := range []string{"input", "editor", "select"} {
		t.Run(method, func(t *testing.T) {
			pipe, sink := permissionFixture()
			pipe.encodeReply = encodeOmpReply
			s := &ompState{}
			p := &ompProc{pipeProc: pipe, state: s}
			wire := map[string]any{"type": "extension_ui_request", "id": "native", "method": method, "title": "Choose", "timeout": 60000}
			if method == "select" {
				wire["options"] = []string{"Alpha", "Beta"}
				wire["optionDetails"] = []map[string]string{{"description": "First"}, {"description": "Second"}}
			}
			req := chatEventData[QuestionRequest](t, s.dialog(pipe, jraw(wire)), EvQuestion)
			if req.ID == "native" || req.ExpiresAt == nil || len(pipe.pendingIn) != 0 {
				t.Fatalf("question snapshot: %+v", req)
			}
			if err := p.Respond(req.ID, true, ""); err == nil {
				t.Fatal("question was available through permission response")
			}
			value := "Typed answer"
			if method == "select" {
				if err := p.AnswerInput(req.ID, []QuestionAnswer{{QuestionID: "value", Values: []string{value}}}, false); err == nil {
					t.Fatal("select accepted arbitrary text")
				}
				value = "Beta"
			}
			if err := p.AnswerInput(req.ID, []QuestionAnswer{{QuestionID: "value", Values: []string{value}}}, false); err != nil {
				t.Fatal(err)
			}
			var reply map[string]any
			if err := json.Unmarshal(sink.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if reply["id"] != "native" || reply["type"] != "extension_ui_response" || reply["value"] != value || reply["confirmed"] != nil {
				t.Fatalf("native dialog reply: %s", sink.String())
			}
			if err := p.AnswerInput(req.ID, nil, true); err == nil {
				t.Fatal("question answered twice")
			}
		})
	}
}

func TestChatOMPWithdrawsDuplicateExpiredAndFinishedQuestions(t *testing.T) {
	for _, mode := range []string{"duplicate", "cancel", "finished", "expired"} {
		t.Run(mode, func(t *testing.T) {
			pipe, _ := permissionFixture()
			s := &ompState{}
			p := &ompProc{pipeProc: pipe, state: s}
			request := `{"type":"extension_ui_request","id":"wire","method":"input","title":"Name"}`
			if mode == "expired" {
				request = `{"type":"extension_ui_request","id":"wire","method":"input","title":"Name","timeout":0}`
			}
			req := chatEventData[QuestionRequest](t, s.dialog(pipe, []byte(request)), EvQuestion)
			var events []Event
			switch mode {
			case "duplicate":
				events = s.dialog(pipe, []byte(`{"type":"extension_ui_request","id":"wire","method":"confirm","title":"Danger","message":"Proceed"}`))
			case "cancel":
				events = s.dialog(pipe, []byte(`{"type":"extension_ui_request","id":"cancel-id","method":"cancel","targetId":"wire"}`))
			case "finished":
				events = s.agentEnd(pipe, []byte(`{"type":"agent_end","isTerminal":true}`))
			}
			if mode != "expired" && !chatHasEvent(events, EvQuestionCancel) {
				t.Fatalf("no withdrawal: %+v", events)
			}
			if p.AnswerInput(req.ID, []QuestionAnswer{{QuestionID: "value", Values: []string{"Alice"}}}, false) == nil || chatHasEvent(events, EvPermission) {
				t.Fatal("stale question reached native protocol")
			}
		})
	}
}

func TestChatOMPObservedContextAndChildState(t *testing.T) {
	s := &ompState{}
	usage := chatEventData[ContextUsage](t, s.response([]byte(`{"type":"response","command":"get_state","success":true,"data":{"sessionId":"s","contextUsage":{"tokens":120,"contextWindow":4000,"percent":3}}}`)), EvUsage)
	if usage.ContextTokens == nil || *usage.ContextTokens != 120 || usage.InputTokens != nil {
		t.Fatalf("OMP state usage: %+v", usage)
	}
	if chatHasEvent(s.response([]byte(`{"type":"response","command":"get_state","success":true,"data":{"sessionId":"s"}}`)), EvUsage) {
		t.Fatal("missing context created fabricated zero usage")
	}
	child := chatEventData[SubagentData](t, s.frame(nil, "subagent_lifecycle", []byte(`{"type":"subagent_lifecycle","payload":{"id":"child","agent":"reviewer","status":"started","description":"Inspect","parentToolCallId":"call","index":0}}`)), EvSubagent)
	if child.ID != "child" || child.Name != "reviewer" || child.Status != "started" || child.ToolCallID != "call" {
		t.Fatalf("OMP child: %+v", child)
	}
}

func chatClaudeQuestion(d *claudeDecoder, p *pipeProc) []Event {
	return d.decode(p, []byte(`{"type":"control_request","request_id":"native","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which sections?","header":"Sections","options":[{"label":"Intro","description":"Opening"},{"label":"Conclusion","description":"Closing"}],"multiSelect":true}],"metadata":{"large_integer":9007199254740993}}}}`))
}

func TestChatClaudeAskUserQuestionNativeControlResponse(t *testing.T) {
	pipe, sink := permissionFixture()
	d := &claudeDecoder{tools: make(map[string]ToolData)}
	p := &claudeProc{pipeProc: pipe, decoder: d}
	req := chatEventData[QuestionRequest](t, chatClaudeQuestion(d, pipe), EvQuestion)
	if len(pipe.pendingIn) != 0 || !req.Questions[0].Multiple || !req.Questions[0].AllowText {
		t.Fatalf("Claude question contract: %+v", req)
	}
	if err := p.Respond(req.ID, true, ""); err == nil {
		t.Fatal("Boolean approval answered a question")
	}
	if err := p.AnswerInput(req.ID, []QuestionAnswer{{QuestionID: "q-0", Values: []string{"Intro", "Conclusion"}}}, false); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
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
	if reply.Type != "control_response" || reply.Response.RequestID != "native" || reply.Response.Subtype != "success" || reply.Response.Response.Behavior != "allow" {
		t.Fatalf("native control envelope: %s", sink.String())
	}
	if !strings.Contains(string(reply.Response.Response.Input), `"Which sections?":["Intro","Conclusion"]`) || !strings.Contains(string(reply.Response.Response.Input), `9007199254740993`) {
		t.Fatalf("input snapshot or native answers were altered: %s", reply.Response.Response.Input)
	}
	if p.AnswerInput(req.ID, nil, true) == nil {
		t.Fatal("question answered twice")
	}
}

func TestChatClaudeQuestionCancellationAndDuplicateBoundary(t *testing.T) {
	for _, mode := range []string{"cancel", "duplicate", "result", "deny"} {
		t.Run(mode, func(t *testing.T) {
			pipe, sink := permissionFixture()
			d := &claudeDecoder{tools: make(map[string]ToolData)}
			p := &claudeProc{pipeProc: pipe, decoder: d}
			req := chatEventData[QuestionRequest](t, chatClaudeQuestion(d, pipe), EvQuestion)
			var events []Event
			switch mode {
			case "cancel":
				events = d.decode(pipe, []byte(`{"type":"control_cancel_request","request_id":"native"}`))
			case "duplicate":
				events = d.decode(pipe, permissionMessage("native", `{"command":"danger"}`))
			case "result":
				events = d.decode(pipe, []byte(`{"type":"result","subtype":"success","result":"done"}`))
			case "deny":
				if err := p.AnswerInput(req.ID, nil, true); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(sink.String(), `"behavior":"deny"`) || strings.Contains(sink.String(), "updatedInput") {
					t.Fatalf("cancel accidentally allowed question tool: %s", sink.String())
				}
			}
			if mode != "deny" && !chatHasEvent(events, EvQuestionCancel) {
				t.Fatalf("missing cancellation: %+v", events)
			}
			if chatHasEvent(events, EvPermission) || p.AnswerInput(req.ID, nil, true) == nil {
				t.Fatal("old snapshot authorized replacement")
			}
		})
	}
}

func TestChatClaudeObservedUsageCitationsAndChildIdentity(t *testing.T) {
	d := &claudeDecoder{tools: make(map[string]ToolData)}
	events := d.decode(nil, []byte(`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg","model":"model","usage":{"input_tokens":100,"cache_read_input_tokens":30,"cache_creation_input_tokens":20,"output_tokens":1},"stop_reason":"refusal","content":[{"type":"text","text":"Answer","citations":[{"type":"web_search_result_location","url":"https://example.com","title":"Source","cited_text":"quoted","encrypted_index":"opaque"}]}]}}`))
	u := chatEventData[ContextUsage](t, events, EvUsage)
	if u.ContextTokens == nil || *u.ContextTokens != 150 || u.OutputTokens != nil || u.ContextWindow != nil {
		t.Fatalf("Claude observed usage: %+v", u)
	}
	if refs := chatEventData[SourcesData](t, events, EvSources); len(refs.Items) != 1 || refs.Items[0].Snippet != "quoted" {
		t.Fatalf("citations: %+v", refs)
	}
	if !chatHasEvent(events, EvGuardrail) {
		t.Fatal("explicit refusal discarded")
	}
	events = d.decode(nil, []byte(`{"type":"result","subtype":"success","usage":{"input_tokens":90000,"output_tokens":40},"modelUsage":{"model":{"contextWindow":200000},"other":{"contextWindow":1000000}}}`))
	u = chatEventData[ContextUsage](t, events, EvUsage)
	if u.ContextTokens != nil || u.ContextWindow == nil || *u.ContextWindow != 200000 || u.OutputTokens == nil || *u.OutputTokens != 40 {
		t.Fatalf("result lifetime totals became occupancy or guessed capacity: %+v", u)
	}
	events = d.decode(nil, []byte(`{"type":"system","subtype":"task_started","task_id":"bash","task_type":"local_bash","description":"agent reviewer"}`))
	if chatHasEvent(events, EvSubagent) {
		t.Fatal("a shell task became a child agent")
	}
	events = d.decode(nil, []byte(`{"type":"system","subtype":"task_started","task_id":"child","task_type":"local_agent","tool_use_id":"call","description":"Review"}`))
	child := chatEventData[SubagentData](t, events, EvSubagent)
	if child.ID != "child" || child.Status != "started" {
		t.Fatalf("child start: %+v", child)
	}
	events = d.decode(nil, []byte(`{"type":"system","subtype":"task_notification","task_id":"child","status":"completed","summary":"Reviewed","output_file":"report.txt"}`))
	child = chatEventData[SubagentData](t, events, EvSubagent)
	if child.Status != "completed" || child.ToolCallID != "call" || !chatHasEvent(events, EvFile) {
		t.Fatalf("child end: %+v", events)
	}
	events = d.decode(nil, []byte(`{"type":"assistant","parent_tool_use_id":"call","message":{"id":"childmsg","usage":{"input_tokens":99},"content":[{"type":"text","text":"Refused https://example.com"}]}}`))
	if chatHasEvent(events, EvUsage) || chatHasEvent(events, EvGuardrail) || chatHasEvent(events, EvSources) {
		t.Fatal("child usage or arbitrary prose invented parent metadata")
	}
}

type chatFailedWriter struct{}

func (chatFailedWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }
func (chatFailedWriter) Close() error              { return nil }

func TestChatInputWriteFailureCannotReplay(t *testing.T) {
	p, _ := chatCodexFixture()
	chatCodexQuestion(p)
	req := chatEventData[QuestionRequest](t, chatDrain(p), EvQuestion)
	p.stdin = chatFailedWriter{}
	if p.AnswerInput(req.ID, nil, true) == nil || len(p.inputs) != 0 || p.AnswerInput(req.ID, nil, true) == nil {
		t.Fatal("uncertain Codex delivery retained replayable request")
	}
	pipe, _ := permissionFixture()
	s := &ompState{}
	omp := &ompProc{pipeProc: pipe, state: s}
	req = chatEventData[QuestionRequest](t, s.dialog(pipe, []byte(`{"type":"extension_ui_request","id":"id","method":"input","title":"Name"}`)), EvQuestion)
	pipe.stdin = chatFailedWriter{}
	if omp.AnswerInput(req.ID, nil, true) == nil || len(s.inputs) != 0 {
		t.Fatal("uncertain OMP delivery retained request")
	}
	pipe, _ = permissionFixture()
	d := &claudeDecoder{tools: make(map[string]ToolData)}
	claude := &claudeProc{pipeProc: pipe, decoder: d}
	req = chatEventData[QuestionRequest](t, chatClaudeQuestion(d, pipe), EvQuestion)
	pipe.stdin = chatFailedWriter{}
	if claude.AnswerInput(req.ID, nil, true) == nil || len(d.inputs) != 0 {
		t.Fatal("uncertain Claude delivery retained request")
	}
}

func TestChatCodexConcurrentAnswerIsSentOnce(t *testing.T) {
	p, sink := chatCodexFixture()
	chatCodexQuestion(p)
	req := chatEventData[QuestionRequest](t, chatDrain(p), EvQuestion)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- p.AnswerInput(req.ID, nil, true)
		}()
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 || strings.Count(sink.String(), "\n") != 1 {
		t.Fatalf("native replies sent %d times: %s", succeeded, sink.String())
	}
}

func TestChatCodexEscapedRequestIDCannotReplaceSnapshot(t *testing.T) {
	p, _ := chatCodexFixture()
	params := json.RawMessage(`{"threadId":"thread","turnId":"turn","itemId":"call","isBlocking":true,"questions":[{"id":"secret","header":"Secret","question":"Token?","isSecret":true,"options":null}]}`)
	p.serverRequest(rpcMsg{ID: json.RawMessage(`"native"`), Method: "item/tool/requestUserInput", Params: params})
	req := chatEventData[QuestionRequest](t, chatDrain(p), EvQuestion)
	if !req.Questions[0].Secret || !req.Questions[0].AllowText {
		t.Fatalf("secret/free-text metadata lost: %+v", req)
	}
	p.serverRequest(rpcMsg{ID: json.RawMessage(`"\u006eative"`), Method: "item/tool/requestUserInput", Params: params})
	events := chatDrain(p)
	if !chatHasEvent(events, EvQuestionCancel) || chatHasEvent(events, EvQuestion) || p.AnswerInput(req.ID, nil, true) == nil {
		t.Fatal("escaped duplicate ID replaced immutable snapshot")
	}
}

func TestChatOMPProviderUsageAndExplicitRefusal(t *testing.T) {
	s := &ompState{}
	events := s.messageEnd([]byte(`{"type":"message_end","messageId":"msg","message":{"role":"assistant","content":[],"usage":{"input":20,"output":5,"cacheRead":10,"cacheWrite":4,"totalTokens":9999,"contextTokens":39},"stopReason":"error","stopDetails":{"type":"refusal","category":"policy","explanation":"Blocked by provider"}}}`))
	u := chatEventData[ContextUsage](t, events, EvUsage)
	if u.ContextTokens == nil || *u.ContextTokens != 39 || u.OutputTokens == nil || *u.OutputTokens != 5 {
		t.Fatalf("OMP billable totals substituted for context: %+v", u)
	}
	notice := chatEventData[GuardrailData](t, events, EvGuardrail)
	if notice.Kind != "refusal" || notice.Message != "Blocked by provider" {
		t.Fatalf("explicit provider refusal: %+v", notice)
	}
	events = s.messageEnd([]byte(`{"type":"message_end","messageId":"msg2","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"refusal-like prose"}}`))
	if chatHasEvent(events, EvGuardrail) {
		t.Fatal("generic OMP error became an invented guardrail")
	}
}
