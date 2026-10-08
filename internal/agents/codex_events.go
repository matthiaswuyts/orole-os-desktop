package agents

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// codexItem is the union of the ThreadItem variants Orole renders (field names follow the
// app-server v2 schema; fields of other variants stay zero).
type codexItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	Text  string `json:"text"`  // agentMessage, plan
	Phase string `json:"phase"` // agentMessage: commentary | final_answer

	Summary []string        `json:"summary"` // reasoning
	Content json.RawMessage `json:"content"` // reasoning: []string (userMessage: inputs, unused)

	Command          string `json:"command"` // commandExecution
	Cwd              string `json:"cwd"`
	Status           string `json:"status"`
	AggregatedOutput string `json:"aggregatedOutput"`
	ExitCode         *int   `json:"exitCode"`

	Changes []codexChange `json:"changes"` // fileChange

	Server    string          `json:"server"` // mcpToolCall, dynamicToolCall
	Tool      string          `json:"tool"`
	Namespace string          `json:"namespace"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result"` // mcpToolCall: object; imageGeneration: base64 string
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
	ContentItems []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"contentItems"`
	Success *bool `json:"success"`

	Query  string          `json:"query"` // webSearch
	Action json.RawMessage `json:"action"`

	Path string `json:"path"` // imageView

	RevisedPrompt *string         `json:"revisedPrompt"` // imageGeneration
	SavedPath     *string         `json:"savedPath"`
	Failure       json.RawMessage `json:"failure"`

	Prompt           *string  `json:"prompt"` // collabAgentToolCall
	ReceiverThreadID []string `json:"receiverThreadIds"`
}

type codexChange struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
	Kind struct {
		Type     string  `json:"type"` // add | delete | update
		MovePath *string `json:"move_path"`
	} `json:"kind"`
}

const (
	codexOutputCap  = 8 << 10 // ToolData.Output limit
	codexDiffCap    = 2 << 10 // per-file diff carried on a tool event
	codexMaxImage   = 25 << 20
	codexItemsCache = 512
)

// notification maps one server notification to normalized events.
func (p *codexProc) notification(method string, raw json.RawMessage) {
	var n struct {
		ThreadID string          `json:"threadId"`
		Item     json.RawMessage `json:"item"`
		ItemID   string          `json:"itemId"`
		Delta    string          `json:"delta"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		WillRetry bool            `json:"willRetry"`
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return
	}
	p.mu.Lock()
	mine := n.ThreadID == "" || n.ThreadID == p.threadID
	p.mu.Unlock()
	switch method {
	case "item/started", "item/completed":
		var it codexItem
		if json.Unmarshal(n.Item, &it) != nil || it.ID == "" {
			return
		}
		p.item(method == "item/completed", it, mine)
	case "item/agentMessage/delta":
		if mine {
			p.liveText(n.ItemID, n.Delta, "Writing: ")
		}
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		if mine {
			p.liveText(n.ItemID, n.Delta, "Thinking: ")
		}
	case "turn/started":
		if !mine {
			return
		}
		p.mu.Lock()
		if p.lastDone != n.Turn.ID {
			p.turn = n.Turn.ID
		}
		p.final = ""
		p.mu.Unlock()
		p.emit(Event{Kind: EvWorking})
	case "turn/completed":
		if mine {
			p.turnCompleted(n.Turn.ID, n.Turn.Status, errorMessage(n.Turn.Error))
		}
	case "error":
		if !mine {
			return
		}
		msg := niceCodexError(n.Error.Message)
		if n.WillRetry {
			p.emit(Event{Kind: EvActivity, Text: truncate("Retrying after an error: "+msg, 160)})
		} else {
			p.emit(Event{Kind: EvError, Text: msg})
		}
	case "serverRequest/resolved":
		p.requestResolved(n.RequestID)
	}
}

func errorMessage(e *struct {
	Message string `json:"message"`
}) string {
	if e == nil {
		return ""
	}
	return niceCodexError(e.Message)
}

// niceCodexError unwraps the provider's JSON error body Codex forwards verbatim.
func niceCodexError(msg string) string {
	msg = strings.TrimSpace(msg)
	if strings.HasPrefix(msg, "{") {
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(msg), &body) == nil {
			if body.Error.Message != "" {
				return body.Error.Message
			}
			if body.Message != "" {
				return body.Message
			}
		}
	}
	return msg
}

// liveText throttles streamed text into one EvActivity line (never one event per token).
func (p *codexProc) liveText(id, delta, label string) {
	p.mu.Lock()
	buf := p.live[id] + delta
	if len(buf) > 2000 {
		buf = buf[len(buf)-2000:]
	}
	p.live[id] = buf
	due := time.Since(p.liveAt) > 750*time.Millisecond
	if due {
		p.liveAt = time.Now()
	}
	p.mu.Unlock()
	if !due {
		return
	}
	line := strings.TrimSpace(buf)
	if i := strings.LastIndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[i:])
	}
	if len(line) > 140 {
		line = "…" + line[len(line)-139:]
	}
	if line != "" {
		p.emit(Event{Kind: EvActivity, Text: label + line})
	}
}

func (p *codexProc) turnCompleted(turnID, status, errMsg string) {
	p.mu.Lock()
	final := p.final
	p.turn, p.lastDone, p.final = "", turnID, ""
	clear(p.items)
	clear(p.live)
	var cancel []string
	for id := range p.pending {
		cancel = append(cancel, id)
		delete(p.pending, id)
	}
	clear(p.byRPC)
	p.mu.Unlock()
	for _, id := range cancel {
		p.emit(Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
	}
	switch status {
	case "completed":
		p.emit(Event{Kind: EvTurnDone, Text: final, Data: jraw(map[string]any{"ok": true, "result": final, "status": status})})
	case "interrupted":
		if p.stopping.Load() {
			p.emit(Event{Kind: EvSystem, Text: "Turn interrupted"})
			return
		}
		p.emit(Event{Kind: EvTurnDone, Text: "Turn interrupted", Data: jraw(map[string]any{"ok": false, "result": "Turn interrupted", "status": status})})
	default:
		if errMsg == "" {
			errMsg = "The turn ended with status " + status
		}
		p.emit(Event{Kind: EvTurnDone, Text: errMsg, Data: jraw(map[string]any{"ok": false, "result": errMsg, "status": status})})
	}
}

// item maps a started or completed ThreadItem. Tool-like items emit EvTool when they start and
// EvToolResult when they finish; text-like items emit once, on completion.
func (p *codexProc) item(done bool, it codexItem, mine bool) {
	if !done {
		p.mu.Lock()
		if len(p.items) >= codexItemsCache {
			clear(p.items)
		}
		p.items[it.ID] = it
		p.mu.Unlock()
	} else {
		p.mu.Lock()
		delete(p.items, it.ID)
		delete(p.live, it.ID)
		p.mu.Unlock()
	}
	if !mine {
		return
	}
	switch it.Type {
	case "agentMessage":
		if !done {
			return
		}
		text := strings.TrimSpace(it.Text)
		if text == "" {
			return
		}
		p.mu.Lock()
		if it.Phase == "final_answer" || p.final == "" || it.Phase == "" {
			p.final = text
		}
		p.mu.Unlock()
		p.emit(Event{Kind: EvMessage, Text: text, Data: jraw(map[string]string{"id": it.ID, "phase": it.Phase})})
	case "plan":
		if done && strings.TrimSpace(it.Text) != "" {
			p.emit(Event{Kind: EvMessage, Text: strings.TrimSpace(it.Text), Data: jraw(map[string]string{"id": it.ID, "phase": "plan"})})
		}
	case "reasoning":
		if !done {
			return
		}
		var content []string
		_ = json.Unmarshal(it.Content, &content)
		var parts []string
		for _, s := range append(append([]string{}, it.Summary...), content...) {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			p.emit(Event{Kind: EvReasoning, Text: strings.Join(parts, "\n\n"), Data: jraw(map[string]string{"id": it.ID})})
		}
	case "contextCompaction":
		if done {
			p.emit(Event{Kind: EvSystem, Text: "Codex compacted the conversation context"})
		}
	case "imageGeneration":
		if done {
			p.imageGenerated(it)
		}
	default:
		if td, ok := codexToolData(it); ok {
			p.toolEvent(done, it, td)
		}
	}
}

func (p *codexProc) toolEvent(done bool, it codexItem, td ToolData) {
	summary := td.Name + ": " + toolSummary(td)
	if !done {
		td.Status = "running"
		p.emit(Event{Kind: EvTool, Text: truncate(summary, 240), Data: jraw(td)})
		return
	}
	text := td.Output
	if text == "" {
		text = td.Status
	}
	if it.Type != "webSearch" {
		td.Input = nil // already sent with the call
	}
	p.emit(Event{Kind: EvToolResult, Text: truncate(text, 400), Data: jraw(td)})
}

func toolSummary(td ToolData) string {
	var in struct {
		Command string `json:"command"`
		Query   string `json:"query"`
		Path    string `json:"path"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	_ = json.Unmarshal(td.Input, &in)
	switch {
	case in.Command != "":
		return oneLine(in.Command, 240)
	case len(in.Changes) > 0:
		paths := make([]string, 0, len(in.Changes))
		for _, c := range in.Changes {
			paths = append(paths, c.Path)
		}
		return oneLine(strings.Join(paths, ", "), 240)
	case in.Query != "":
		return oneLine(in.Query, 240)
	case in.Path != "":
		return in.Path
	}
	return oneLine(string(td.Input), 240)
}

func oneLine(s string, n int) string { return truncate(strings.Join(strings.Fields(s), " "), n) }

// codexToolData converts a tool-like item to ToolData (status filled from the item: running while
// in progress, then ok or error). ok is false for items that are not tools.
func codexToolData(it codexItem) (ToolData, bool) {
	td := ToolData{ID: it.ID, Status: "running"}
	finished := it.Status != "" && it.Status != "inProgress" && it.Status != "in_progress"
	switch it.Type {
	case "commandExecution":
		td.Name = "shell"
		td.Input = jraw(map[string]string{"command": it.Command, "cwd": it.Cwd})
		if finished {
			out := it.AggregatedOutput
			if it.ExitCode != nil && *it.ExitCode != 0 {
				out = fmt.Sprintf("exit code %d\n%s", *it.ExitCode, out)
			}
			if it.Status == "declined" {
				out = "declined"
			}
			td.Output = truncate(out, codexOutputCap)
			td.Status = okIf(it.Status == "completed")
		}
	case "fileChange":
		td.Name = "apply_patch"
		td.Input = jraw(map[string]any{"changes": codexChangeViews(it.Changes, false)})
		if finished {
			td.Output = fileChangeOutput(it)
			td.Status = okIf(it.Status == "completed")
		}
	case "mcpToolCall":
		td.Name, td.Server = it.Tool, it.Server
		td.Input = rawOrNull(it.Arguments)
		if finished {
			td.Output = truncate(mcpResultText(it), codexOutputCap)
			td.Status = okIf(it.Status == "completed")
		}
	case "dynamicToolCall":
		td.Name = it.Tool
		td.Input = rawOrNull(it.Arguments)
		if finished {
			var parts []string
			for _, c := range it.ContentItems {
				if c.Type == "inputText" && c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
			td.Output = truncate(strings.Join(parts, "\n"), codexOutputCap)
			td.Status = okIf(it.Status == "completed" && (it.Success == nil || *it.Success))
		}
	case "webSearch":
		td.Name = "web_search"
		td.Input = jraw(map[string]any{"query": it.Query, "action": rawOrNull(it.Action)})
		if it.Query != "" || len(it.Action) > 0 {
			td.Output = truncate(it.Query, codexOutputCap)
		}
		td.Status = "running"
		if finished || it.Query != "" {
			td.Status = "ok"
		}
	case "imageView":
		td.Name = "view_image"
		td.Input = jraw(map[string]string{"path": it.Path})
		td.Status = "ok"
	case "collabAgentToolCall":
		td.Name = "agent_" + it.Tool
		in := map[string]any{"receiverThreadIds": it.ReceiverThreadID}
		if it.Prompt != nil {
			in["prompt"] = *it.Prompt
		}
		td.Input = jraw(in)
		if finished {
			td.Status = okIf(it.Status == "completed")
		}
	default:
		return ToolData{}, false
	}
	return td, true
}

func okIf(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}

func rawOrNull(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage("null")
	}
	return r
}

func mcpResultText(it codexItem) string {
	if it.Error != nil && it.Error.Message != "" {
		return it.Error.Message
	}
	var res struct {
		Content           []json.RawMessage `json:"content"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
	}
	if json.Unmarshal(it.Result, &res) != nil {
		return ""
	}
	var parts []string
	for _, c := range res.Content {
		var b struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(c, &b) == nil && b.Type == "text" {
			parts = append(parts, b.Text)
		} else if b.Type != "" {
			parts = append(parts, "["+b.Type+" content]")
		}
	}
	if len(parts) == 0 && len(res.StructuredContent) > 0 && string(res.StructuredContent) != "null" {
		return string(res.StructuredContent)
	}
	return strings.Join(parts, "\n")
}

type codexChangeView struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	MovePath      string `json:"movePath,omitempty"`
	Diff          string `json:"diff,omitempty"`
	DiffTruncated bool   `json:"diffTruncated,omitempty"`
}

// codexChangeViews lists the changed paths; withDiff also carries each (truncated) diff.
func codexChangeViews(changes []codexChange, withDiff bool) []codexChangeView {
	out := make([]codexChangeView, 0, len(changes))
	for _, c := range changes {
		v := codexChangeView{Path: c.Path, Kind: c.Kind.Type}
		if c.Kind.MovePath != nil {
			v.MovePath = *c.Kind.MovePath
		}
		if withDiff && c.Diff != "" {
			v.Diff = truncate(c.Diff, codexDiffCap)
			v.DiffTruncated = len(c.Diff) > codexDiffCap
		}
		out = append(out, v)
	}
	return out
}

func fileChangeOutput(it codexItem) string {
	var b strings.Builder
	b.WriteString(it.Status)
	for _, c := range it.Changes {
		b.WriteString("\n" + c.Kind.Type + " " + c.Path)
	}
	return truncate(b.String(), codexOutputCap)
}

// ---- generated images ----

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// imageKind identifies an image by magic bytes ("" = not an image Orole serves).
func imageKind(b []byte) (mime, ext string) {
	switch {
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png", ".png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg", ".jpg"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp", ".webp"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif", ".gif"
	}
	return "", ""
}

// imageGenerated saves a finished imageGeneration item under the session's image directory and
// reports it as EvImage. The item's result is the image as base64 (observed live); the path Codex
// saved it to is the fallback.
func (p *codexProc) imageGenerated(it codexItem) {
	prompt := ""
	if it.RevisedPrompt != nil {
		prompt = strings.TrimSpace(*it.RevisedPrompt)
	}
	if it.Status == "failed" || (len(it.Failure) > 0 && string(it.Failure) != "null") {
		reason := "image generation failed"
		var f struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(it.Failure, &f) == nil && f.Type == "usageLimitExceeded" {
			reason = "image generation hit a usage limit"
		}
		p.emit(Event{Kind: EvError, Text: "Codex: " + reason})
		return
	}
	data, err := decodeCodexImage(it)
	if err != nil {
		p.emit(Event{Kind: EvError, Text: "Codex image could not be read: " + err.Error()})
		return
	}
	mime, ext := imageKind(data)
	if mime == "" {
		p.emit(Event{Kind: EvError, Text: "Codex image result is not a PNG, JPEG, WebP or GIF image"})
		return
	}
	if p.imageDir == "" {
		p.emit(Event{Kind: EvError, Text: "Codex generated an image but this session has no image directory"})
		return
	}
	if err := os.MkdirAll(p.imageDir, 0o700); err != nil {
		p.emit(Event{Kind: EvError, Text: "Codex image could not be saved: " + err.Error()})
		return
	}
	name := unsafeNameChars.ReplaceAllString(it.ID, "_")
	path := filepath.Join(p.imageDir, name+ext)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		err = os.Rename(tmp, path)
	} else {
		_ = os.Remove(tmp)
	}
	if _, err := os.Stat(path); err != nil {
		p.emit(Event{Kind: EvError, Text: "Codex image could not be saved: " + err.Error()})
		return
	}
	p.emit(Event{Kind: EvImage, Text: prompt, Data: jraw(ImageData{ID: it.ID, Path: path, Mime: mime, Prompt: prompt})})
}

func decodeCodexImage(it codexItem) ([]byte, error) {
	var res string
	_ = json.Unmarshal(it.Result, &res)
	res = strings.TrimSpace(res)
	if i := strings.Index(res, ";base64,"); strings.HasPrefix(res, "data:") && i > 0 {
		res = res[i+len(";base64,"):]
	}
	if res != "" && base64.StdEncoding.EncodedLen(codexMaxImage) >= len(res) {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, err := enc.DecodeString(res); err == nil {
				return b, nil
			}
		}
	}
	if it.SavedPath != nil && filepath.IsAbs(*it.SavedPath) {
		f, err := os.Open(*it.SavedPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > codexMaxImage {
			return nil, fmt.Errorf("saved image is not a regular file under %d MiB", codexMaxImage>>20)
		}
		b := make([]byte, info.Size())
		if _, err := io.ReadFull(f, b); err != nil {
			return nil, err
		}
		return b, nil
	}
	if res == "" {
		return nil, fmt.Errorf("the item carries no image data")
	}
	return nil, fmt.Errorf("the image data is not base64 or larger than %d MiB", codexMaxImage>>20)
}
