package openai

import (
	"slices"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// capsTableVersion identifies the data below. GET /v1/models lists only ids, so model
// capabilities come from this table, transcribed from the per-model pages at
// developers.openai.com/api/docs/models/<id> (context window, input modalities, supported
// tools, reasoning.effort values). Update the table and bump the version together; a model
// whose id matches no entry is not offered.
const capsTableVersion = "2026-10-09"

// family describes every model id starting with prefix; the longest matching prefix wins.
type family struct {
	prefix  string
	context int64
	vision  bool // image and PDF (input_file) input
	tools   bool // function calling
	web     bool // hosted web_search
	deep    bool // native deep-research model
	effort  []string
}

var (
	effNone      = []string{"none", "low", "medium", "high", "xhigh", "max"}
	effLow       = []string{"low", "medium", "high", "xhigh", "max"}
	effNoneXHigh = []string{"none", "low", "medium", "high", "xhigh"}
	effNoneHigh  = []string{"none", "low", "medium", "high"}
	effCodex     = []string{"low", "medium", "high", "xhigh"}
	effCodexOld  = []string{"low", "medium", "high"}
	effPro       = []string{"medium", "high", "xhigh"}
	effGPT5      = []string{"minimal", "low", "medium", "high"}
	effO         = []string{"low", "medium", "high"}
)

const (
	ctx1M   = 1_050_000
	ctx400k = 400_000
	ctx200k = 200_000
	ctx128k = 128_000
)

var families = []family{
	{prefix: "gpt-6-astra", context: ctx1M, vision: true, tools: true, web: true, effort: effLow},
	{prefix: "gpt-6.1", context: ctx1M, vision: true, tools: true, web: true, effort: effLow},
	{prefix: "gpt-6", context: ctx1M, vision: true, tools: true, web: true, effort: effNone},
	{prefix: "gpt-5.6", context: ctx1M, vision: true, tools: true, web: true, effort: effNone},
	{prefix: "gpt-5.5-pro", context: ctx1M, vision: true, tools: true, web: true, effort: effPro},
	{prefix: "gpt-5.5", context: ctx1M, vision: true, tools: true, web: true, effort: effNoneXHigh},
	{prefix: "gpt-5.4-pro", context: ctx1M, vision: true, tools: true, web: true, effort: effPro},
	{prefix: "gpt-5.4-mini", context: ctx400k, vision: true, tools: true, web: true, effort: effNoneXHigh},
	{prefix: "gpt-5.4-nano", context: ctx400k, vision: true, tools: true, web: true, effort: effNoneXHigh},
	{prefix: "gpt-5.4", context: ctx1M, vision: true, tools: true, web: true, effort: effNoneXHigh},
	{prefix: "gpt-5.3-codex", context: ctx400k, vision: true, tools: true, web: true, effort: effCodex},
	{prefix: "gpt-5.3-chat-latest", context: ctx128k, vision: true, tools: true, web: true},
	{prefix: "gpt-5.2-pro", context: ctx400k, vision: true, tools: true, web: true, effort: effPro},
	{prefix: "gpt-5.2-codex", context: ctx400k, vision: true, tools: true, web: true, effort: effCodex},
	{prefix: "gpt-5.2-chat-latest", context: ctx128k, vision: true, tools: true, web: true},
	{prefix: "gpt-5.2", context: ctx400k, vision: true, tools: true, web: true, effort: effNoneXHigh},
	{prefix: "gpt-5.1-codex-max", context: ctx400k, vision: true, tools: true, web: true, effort: effCodex},
	{prefix: "gpt-5.1-codex", context: ctx400k, vision: true, tools: true, web: true, effort: effCodexOld},
	{prefix: "gpt-5.1-chat-latest", context: ctx128k, vision: true, tools: true, web: true},
	{prefix: "gpt-5.1", context: ctx400k, vision: true, tools: true, web: true, effort: effNoneHigh},
	{prefix: "gpt-5-pro", context: ctx400k, vision: true, tools: true, web: true, effort: []string{"high"}},
	{prefix: "gpt-5-codex", context: ctx400k, vision: true, tools: true, web: true, effort: effCodexOld},
	{prefix: "gpt-5-chat-latest", context: ctx128k, vision: true, tools: true, web: true},
	{prefix: "gpt-5", context: ctx400k, vision: true, tools: true, web: true, effort: effGPT5},
	{prefix: "chat-latest", context: ctx400k, vision: true, tools: true, web: true},
	{prefix: "gpt-4.1-nano", context: 1_047_576, vision: true, tools: true},
	{prefix: "gpt-4.1", context: 1_047_576, vision: true, tools: true, web: true},
	{prefix: "gpt-4o", context: ctx128k, vision: true, tools: true, web: true},
	{prefix: "o4-mini-deep-research", context: ctx200k, vision: true, web: true, deep: true},
	{prefix: "o3-deep-research", context: ctx200k, vision: true, web: true, deep: true},
	{prefix: "o4-mini", context: ctx200k, vision: true, tools: true, web: true, effort: effO},
	{prefix: "o3-pro", context: ctx200k, vision: true, tools: true, web: true, effort: effO},
	{prefix: "o3-mini", context: ctx200k, tools: true, effort: effO},
	{prefix: "o3", context: ctx200k, vision: true, tools: true, web: true, effort: effO},
	{prefix: "o1", context: ctx200k, vision: true, tools: true, effort: effO},
}

// excluded marks ids that match a family prefix but are not text chat models on the
// Responses API (audio, realtime, image generation, search-only Chat Completions models...).
var excluded = []string{
	"audio", "realtime", "transcribe", "tts", "image", "search", "live", "diarize", "moderation",
	"embedding", "instruct", "computer-use", "-preview", "cyber", "daybreak", "rosalind", "whisper",
}

// lookup returns the family for a model id.
func lookup(id string) (family, bool) {
	for _, x := range excluded {
		if strings.Contains(id, x) {
			return family{}, false
		}
	}
	best := -1
	for i := range families {
		if strings.HasPrefix(id, families[i].prefix) && (best < 0 || len(families[i].prefix) > len(families[best].prefix)) {
			best = i
		}
	}
	if best < 0 {
		return family{}, false
	}
	return families[best], true
}

// modelFor builds the catalogue entry for a model id.
func modelFor(id string) (llm.Model, bool) {
	f, ok := lookup(id)
	if !ok {
		return llm.Model{}, false
	}
	m := llm.Model{
		ID:   id,
		Name: id,
		Caps: llm.Caps{
			Tools: f.tools, Vision: f.vision, PDF: f.vision, Reasoning: len(f.effort) > 0 || f.deep,
			DeepResearch: f.deep, ContextWindow: f.context,
		},
		Reasoning: slices.Clone(f.effort),
	}
	if f.web {
		m.Caps.HostedTools = []string{llm.HostedWebSearch}
	}
	return m, true
}
