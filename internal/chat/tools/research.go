package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// ResearchName is the function name of the research tool.
const ResearchName = "research"

// Research depths.
const (
	DepthQuick    = "quick"
	DepthStandard = "standard"
	DepthDeep     = "deep"
)

// Budget bounds one research run. Token accounting uses provider-reported usage and is checked
// between model calls; planning and workers stop at 85% of MaxTokens and 80% of WallClock so the
// synthesis always has room left.
type Budget struct {
	SubQuestions int           // sub-questions planned (3-6)
	WorkerSteps  int           // model calls per worker, including the final no-tools wrap-up
	MaxSources   int           // distinct sources collected
	MaxTokens    int64         // provider-reported input+output tokens over the whole run
	WallClock    time.Duration // whole run
	SynthTokens  int           // output cap of the synthesis call
}

// DefaultBudgets are the depth presets.
var DefaultBudgets = map[string]Budget{
	DepthQuick:    {SubQuestions: 3, WorkerSteps: 3, MaxSources: 8, MaxTokens: 60_000, WallClock: 2 * time.Minute, SynthTokens: 2000},
	DepthStandard: {SubQuestions: 5, WorkerSteps: 5, MaxSources: 20, MaxTokens: 250_000, WallClock: 6 * time.Minute, SynthTokens: 4000},
	DepthDeep:     {SubQuestions: 6, WorkerSteps: 8, MaxSources: 40, MaxTokens: 800_000, WallClock: 15 * time.Minute, SynthTokens: 8000},
}

// Providers is the part of llm.Service the research loop needs.
type Providers interface {
	Provider(ctx context.Context, id string) (llm.Provider, error)
	Defaults(ctx context.Context) (llm.Defaults, error)
}

// Source is one cited web source.
type Source struct {
	URL     string
	Title   string
	Snippet string
}

// Report is a finished research report. Markdown cites Sources as [1], [2], ... in order.
type Report struct {
	Title    string
	Markdown string
	Sources  []Source
}

// Native lets a provider-side research model (for example OpenAI deep research) replace the
// provider-agnostic loop. Research uses it when it is set, the model reports Caps.DeepResearch and
// Supports accepts the provider. No implementation ships with the daemon yet.
type Native interface {
	Supports(provider string, caps llm.Caps) bool
	Research(ctx context.Context, rc chat.RunContext, question, depth string) (Report, error)
}

// Research is the "research" tool: plan sub-questions, search and read in parallel workers, then
// write a cited report. It talks to the model directly and never writes to memory.
type Research struct {
	LLM Providers
	// Fetcher runs fetch_url for models whose endpoint has no hosted fetch. Nil: no fetch fallback.
	Fetcher *Fetcher
	// Native is optional; see Native.
	Native Native
	// Presets overrides DefaultBudgets (tests).
	Presets map[string]Budget
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

// workerParallelism is the number of sub-questions researched at once.
const workerParallelism = 3

func (*Research) ID() string { return chat.ToolResearch }

func (r *Research) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if _, ok := botTool(bot, chat.ToolResearch); !ok || !caps.Tools {
		return nil
	}
	if !hasHosted(caps, llm.HostedWebSearch) && !(r.Native != nil && caps.DeepResearch && r.Native.Supports(bot.Provider, caps)) {
		return nil
	}
	return []llm.ToolSpec{{
		Name: ResearchName,
		Description: "Research a question in depth: plan sub-questions, search the web and read pages, then write a cited markdown report. " +
			"Slow and expensive; use it only when the user asks for research or a thorough, sourced answer. Depth: quick (about 2 min), standard (about 6 min), deep (about 15 min).",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"question":{"type":"string","description":"The research question, with all context the researcher needs"},` +
			`"depth":{"type":"string","enum":["quick","standard","deep"],"description":"How much effort to spend; default standard"}},` +
			`"required":["question"],"additionalProperties":false}`),
	}}
}

func (r *Research) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Research) budget(depth string) (Budget, bool) {
	presets := r.Presets
	if presets == nil {
		presets = DefaultBudgets
	}
	b, ok := presets[depth]
	return b, ok
}

func errResult(format string, a ...any) (chat.ToolResult, error) {
	return chat.ToolResult{Text: fmt.Sprintf(format, a...), IsError: true}, nil
}

func (r *Research) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if name != ResearchName {
		return chat.ToolResult{}, errHostedCall
	}
	var a struct {
		Question string `json:"question"`
		Depth    string `json:"depth"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	question := strings.TrimSpace(untrusted.Text(a.Question, 4000))
	if question == "" {
		return errResult("invalid arguments: question is required")
	}
	if a.Depth == "" {
		a.Depth = DepthStandard
	}
	budget, ok := r.budget(a.Depth)
	if !ok {
		return errResult("invalid arguments: depth must be quick, standard or deep")
	}
	if r.LLM == nil {
		return errResult("research is not available: no model service")
	}
	defaults, err := r.LLM.Defaults(ctx)
	if err != nil {
		return errResult("research is not available: %v", err)
	}
	providerID, model := rc.Bot.Provider, rc.Bot.Model
	if providerID == "" || model == "" {
		var ok bool
		if providerID, model, ok = llm.SplitModel(defaults.ChatModel); !ok {
			return errResult("research is not available: no model is selected")
		}
	}
	prov, err := r.LLM.Provider(ctx, providerID)
	if err != nil {
		return errResult("research is not available: %v", err)
	}

	if r.Native != nil && rc.Caps.DeepResearch && r.Native.Supports(providerID, rc.Caps) {
		rep, err := r.Native.Research(ctx, rc, question, a.Depth)
		if err != nil {
			if ctx.Err() != nil {
				return chat.ToolResult{}, ctx.Err()
			}
			return errResult("research failed: %s", untrusted.Line(llm.AsError(err).Message, 300))
		}
		return assemble(rep, ""), nil
	}
	if !hasHosted(rc.Caps, llm.HostedWebSearch) {
		return errResult("research is not available: this model's endpoint cannot run web search")
	}

	run := &researchRun{
		r: r, rc: rc, budget: budget, question: question, depth: a.Depth,
		prov: prov, model: model, caps: rc.Caps,
		wprov: prov, wmodel: model, wcaps: rc.Caps,
		srcSet: map[string]struct{}{},
	}
	run.configureTools()
	run.pickWorkerModel(ctx, defaults.MemoryModel)
	rep, notice, err := run.execute(ctx)
	if err != nil {
		var le *llm.Error
		switch {
		case ctx.Err() != nil:
			return chat.ToolResult{}, ctx.Err()
		case errors.As(err, &le):
			return errResult("research failed: %s", untrusted.Line(le.Message, 300))
		}
		return errResult("research failed: %s", untrusted.Line(err.Error(), 300))
	}
	return assemble(rep, notice), nil
}

var citeRE = regexp.MustCompile(`\[(\d{1,4})\]`)

// dropUnknownCitations removes [n] markers that do not name one of the n sources.
func dropUnknownCitations(md string, sources int) string {
	return citeRE.ReplaceAllStringFunc(md, func(m string) string {
		n, _ := strconv.Atoi(m[1 : len(m)-1])
		if n < 1 || n > sources {
			return ""
		}
		return m
	})
}

// assemble builds the tool result. The model-facing text and the report part carry the same markdown
// (partial-run note, body with [n] citations, numbered source list) so the engine shows it once; the
// citation parts carry the sources for the UI. The text comes from web content, so it is untrusted.
func assemble(rep Report, notice string) chat.ToolResult {
	body := dropUnknownCitations(strings.TrimSpace(rep.Markdown), len(rep.Sources))
	title := untrusted.Line(rep.Title, 200)
	if title == "" {
		title = "Research report"
	}
	var text strings.Builder
	if notice != "" {
		text.WriteString("> Note: " + notice + "\n\n")
	}
	text.WriteString(body)
	if len(rep.Sources) > 0 {
		text.WriteString("\n\n## Sources\n")
		for i, s := range rep.Sources {
			label := s.Title
			if label == "" {
				label = s.URL
			}
			fmt.Fprintf(&text, "%d. %s - %s\n", i+1, label, s.URL)
		}
	}
	full := strings.TrimSpace(text.String())
	parts := make([]chat.MsgPart, 0, len(rep.Sources)+1)
	parts = append(parts, chat.MsgPart{Type: chat.PartReport, Title: title, Text: full})
	for _, s := range rep.Sources {
		parts = append(parts, chat.MsgPart{Type: chat.PartCitation, URL: s.URL, Title: s.Title, Snippet: s.Snippet})
	}
	return chat.ToolResult{Text: full, Untrusted: true, Parts: parts}
}

// sourceKey identifies a source for de-duplication: scheme, host, path and query, no fragment.
func sourceKey(raw string) (key, clean string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(raw) > maxURLLen {
		return "", "", false
	}
	u.Fragment = ""
	clean = u.String()
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.Scheme = "https"
	return u.String(), clean, true
}
