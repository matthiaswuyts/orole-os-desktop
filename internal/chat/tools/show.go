package tools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Names of the structured-output tools. Each one renders one chat component (the frontend maps the
// part's Name to the component), so the model answers with typed data instead of formatted text.
const (
	ShowTable      = "show_table"
	ShowChart      = "show_chart"
	ShowDiagram    = "show_diagram"
	ShowFlow       = "show_flow_graph"
	ShowMath       = "show_math"
	ShowSpecSheet  = "show_spec_sheet"
	ShowComparison = "show_comparison"
	ShowTimeline   = "show_timeline"
	ShowJob        = "show_job_progress"
)

// ShowPrompt is appended to every bot's system prompt that can call the show_* tools.
const ShowPrompt = `## Structured output

Besides Markdown text you can show typed components with the show_* tools: show_table (tabular data), show_chart (one numeric series), show_diagram (boxes and arrows on a canvas), show_flow_graph (steps in columns and rows with progress state), show_math (worked steps of a calculation), show_spec_sheet (facts about one thing), show_comparison (options against traits with a recommendation), show_timeline (events in order), show_job_progress (staged work and its progress).

Use one when the content is genuinely structured data, and prefer it to a Markdown table or an ASCII drawing. Call it before the sentence that refers to it, keep the surrounding text short, and do not repeat what the component already shows. Write maths with plain Unicode (x², √, ½, ≤), never LaTeX. Never put instructions or untrusted text you have not read into a component.`

// Show offers the show_* tools to every bot whose model can call tools.
type Show struct{}

func (Show) ID() string { return "show" }

type showTool struct {
	name, desc, schema string
	check              func(raw json.RawMessage) (any, error)
}

func (Show) Specs(_ chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools {
		return nil
	}
	out := make([]llm.ToolSpec, 0, len(showTools))
	for _, t := range showTools {
		out = append(out, llm.ToolSpec{Name: t.name, Description: t.desc, Schema: json.RawMessage(t.schema)})
	}
	return out
}

func (Show) Call(_ context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	for _, t := range showTools {
		if t.name != name {
			continue
		}
		v, err := t.check(args)
		if err != nil {
			return errResult("invalid %s arguments: %s", name, err)
		}
		spec, err := json.Marshal(v)
		if err != nil || len(spec) > maxShowBytes {
			return errResult("the component is too large")
		}
		return chat.ToolResult{
			Text:  "Shown to the user. Do not repeat its contents in text.",
			Parts: []chat.MsgPart{{Type: chat.PartUI, ID: name, Name: kindOf(name), Spec: string(spec)}},
		}, nil
	}
	return errResult("unknown tool")
}

const maxShowBytes = 64 << 10

func kindOf(tool string) string {
	switch tool {
	case ShowTable:
		return "table"
	case ShowChart:
		return "chart"
	case ShowDiagram:
		return "diagram"
	case ShowFlow:
		return "flow"
	case ShowMath:
		return "math"
	case ShowSpecSheet:
		return "spec"
	case ShowComparison:
		return "comparison"
	case ShowTimeline:
		return "timeline"
	}
	return "job"
}

// decode parses args strictly into v: unknown fields are rejected so a typo is reported to the model.
func decode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// text checks a display string: valid UTF-8, no control characters, at most max runes.
func text(field, s string, max int, required bool) error {
	if required && strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return fmt.Errorf("%s is invalid or longer than %d characters", field, max)
	}
	for _, r := range s {
		if r < 0x20 && r != '\n' {
			return fmt.Errorf("%s contains control characters", field)
		}
	}
	return nil
}

func count(field string, n, min, max int) error {
	if n < min || n > max {
		return fmt.Errorf("%s needs between %d and %d entries, got %d", field, min, max, n)
	}
	return nil
}

func texts(field string, items []string, max int) error {
	for _, s := range items {
		if err := text(field, s, max, false); err != nil {
			return err
		}
	}
	return nil
}

// ---- table ----

type tableFormat struct {
	Kind       string `json:"kind"`
	Decimals   *int   `json:"decimals,omitempty"`
	Compact    bool   `json:"compact,omitempty"`
	Unit       string `json:"unit,omitempty"`
	Currency   string `json:"currency,omitempty"`
	Basis      string `json:"basis,omitempty"`
	Style      string `json:"style,omitempty"`
	Max        *int   `json:"max,omitempty"`
	TrueLabel  string `json:"trueLabel,omitempty"`
	FalseLabel string `json:"falseLabel,omitempty"`
}

type tableColumn struct {
	Key      string       `json:"key"`
	Label    string       `json:"label"`
	Format   *tableFormat `json:"format,omitempty"`
	Priority string       `json:"priority,omitempty"`
}

type tableSpec struct {
	Caption string           `json:"caption,omitempty"`
	Columns []tableColumn    `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

var (
	currencyRe  = regexp.MustCompile(`^[A-Z]{3}$`)
	formatKinds = map[string]bool{"text": true, "number": true, "currency": true, "percent": true, "delta": true, "date": true, "boolean": true, "badge": true, "list": true}
)

func checkTable(raw json.RawMessage) (any, error) {
	var s tableSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := text("caption", s.Caption, 120, false); err != nil {
		return nil, err
	}
	if err := count("columns", len(s.Columns), 1, 12); err != nil {
		return nil, err
	}
	if err := count("rows", len(s.Rows), 0, 100); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for i := range s.Columns {
		c := &s.Columns[i]
		if err := text("column key", c.Key, 40, true); err != nil {
			return nil, err
		}
		if keys[c.Key] {
			return nil, fmt.Errorf("duplicate column key %q", c.Key)
		}
		keys[c.Key] = true
		if err := text("column label", c.Label, 60, true); err != nil {
			return nil, err
		}
		if c.Priority != "" && c.Priority != "primary" && c.Priority != "secondary" {
			return nil, fmt.Errorf("column priority must be primary or secondary")
		}
		if f := c.Format; f != nil {
			if !formatKinds[f.Kind] {
				return nil, fmt.Errorf("unknown column format %q", f.Kind)
			}
			if f.Kind == "currency" && !currencyRe.MatchString(f.Currency) {
				return nil, fmt.Errorf("currency format needs an ISO 4217 code such as USD")
			}
			if (f.Decimals != nil && (*f.Decimals < 0 || *f.Decimals > 8)) || (f.Max != nil && (*f.Max < 1 || *f.Max > 10)) {
				return nil, fmt.Errorf("decimals must be 0-8 and max 1-10")
			}
			if (f.Basis != "" && f.Basis != "fraction" && f.Basis != "unit") || (f.Style != "" && f.Style != "date" && f.Style != "datetime" && f.Style != "relative") {
				return nil, fmt.Errorf("invalid percent basis or date style")
			}
			if err := texts("format", []string{f.Unit, f.TrueLabel, f.FalseLabel}, 16); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range s.Rows {
		for k, v := range row {
			if !keys[k] {
				delete(row, k)
				continue
			}
			if err := checkCell(v); err != nil {
				return nil, fmt.Errorf("row value %q: %w", k, err)
			}
		}
	}
	return s, nil
}

// checkCell accepts null, a string, a number, a boolean or a short list of strings and numbers.
func checkCell(v any) error {
	switch x := v.(type) {
	case nil, bool, float64:
		return nil
	case string:
		return text("value", x, 400, false)
	case []any:
		if len(x) > 20 {
			return fmt.Errorf("lists are limited to 20 items")
		}
		for _, e := range x {
			switch y := e.(type) {
			case float64:
			case string:
				if err := text("value", y, 100, false); err != nil {
					return err
				}
			default:
				return fmt.Errorf("lists hold strings and numbers only")
			}
		}
		return nil
	}
	return fmt.Errorf("values are strings, numbers, booleans or lists of them")
}

// ---- chart ----

type chartSpec struct {
	Label    string    `json:"label"`
	Value    string    `json:"value"`
	Delta    string    `json:"delta,omitempty"`
	Trend    string    `json:"trend,omitempty"`
	UpIsGood *bool     `json:"upIsGood,omitempty"`
	Points   []float64 `json:"points"`
	Variant  string    `json:"variant,omitempty"`
}

func checkChart(raw json.RawMessage) (any, error) {
	var s chartSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	for _, f := range []struct {
		n, v string
		max  int
		req  bool
	}{{"label", s.Label, 60, true}, {"value", s.Value, 30, true}, {"delta", s.Delta, 30, false}} {
		if err := text(f.n, f.v, f.max, f.req); err != nil {
			return nil, err
		}
	}
	if err := count("points", len(s.Points), 1, 200); err != nil {
		return nil, err
	}
	if s.Trend != "" && s.Trend != "up" && s.Trend != "down" && s.Trend != "flat" {
		return nil, fmt.Errorf("trend must be up, down or flat")
	}
	if s.Variant != "" && s.Variant != "area" && s.Variant != "line" && s.Variant != "bars" {
		return nil, fmt.Errorf("variant must be area, line or bars")
	}
	return s, nil
}

// ---- diagram ----

type diagramNode struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
	Shape  string  `json:"shape,omitempty"`
}

type diagramLink struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

type diagramSpec struct {
	Title string        `json:"title"`
	Nodes []diagramNode `json:"nodes"`
	Links []diagramLink `json:"links"`
}

const canvasW, canvasH = 600, 400

func checkDiagram(raw json.RawMessage) (any, error) {
	var s diagramSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := text("title", s.Title, 80, true); err != nil {
		return nil, err
	}
	if err := count("nodes", len(s.Nodes), 1, 40); err != nil {
		return nil, err
	}
	if err := count("links", len(s.Links), 0, 80); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for i := range s.Nodes {
		n := &s.Nodes[i]
		if err := text("node id", n.ID, 40, true); err != nil {
			return nil, err
		}
		if ids[n.ID] {
			return nil, fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
		if err := text("node label", n.Label, 60, true); err != nil {
			return nil, err
		}
		if n.Width == 0 {
			n.Width = 120
		}
		if n.Height == 0 {
			n.Height = 44
		}
		if n.Width < 40 || n.Width > 300 || n.Height < 24 || n.Height > 160 {
			return nil, fmt.Errorf("node size must be 40-300 wide and 24-160 high")
		}
		if n.X < 0 || n.Y < 0 || n.X+n.Width > canvasW || n.Y+n.Height > canvasH {
			return nil, fmt.Errorf("node %q lies outside the %dx%d canvas (x and y are the top-left corner)", n.ID, canvasW, canvasH)
		}
		if n.Shape != "" && n.Shape != "box" && n.Shape != "round" && n.Shape != "pill" {
			return nil, fmt.Errorf("shape must be box, round or pill")
		}
	}
	for _, l := range s.Links {
		if !ids[l.From] || !ids[l.To] {
			return nil, fmt.Errorf("link %q -> %q names an unknown node", l.From, l.To)
		}
		if err := text("link label", l.Label, 40, false); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ---- flow graph ----

type flowNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Column int    `json:"column"`
	Row    int    `json:"row"`
	State  string `json:"state"`
}

type flowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type flowSpec struct {
	Nodes []flowNode `json:"nodes"`
	Edges []flowEdge `json:"edges"`
}

func checkFlow(raw json.RawMessage) (any, error) {
	var s flowSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := count("nodes", len(s.Nodes), 1, 30); err != nil {
		return nil, err
	}
	if err := count("edges", len(s.Edges), 0, 60); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, n := range s.Nodes {
		if err := text("node id", n.ID, 40, true); err != nil {
			return nil, err
		}
		if ids[n.ID] {
			return nil, fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
		if err := text("node label", n.Label, 24, true); err != nil {
			return nil, err
		}
		if n.Column < 0 || n.Column > 9 || n.Row < 0 || n.Row > 7 {
			return nil, fmt.Errorf("column is 0-9 and row 0-7")
		}
		if n.State != "done" && n.State != "active" && n.State != "pending" {
			return nil, fmt.Errorf("state must be done, active or pending")
		}
	}
	for _, e := range s.Edges {
		if !ids[e.From] || !ids[e.To] {
			return nil, fmt.Errorf("edge %q -> %q names an unknown node", e.From, e.To)
		}
	}
	return s, nil
}

// ---- math ----

type mathStep struct {
	Expression string `json:"expression"`
	Note       string `json:"note,omitempty"`
}

type mathSpec struct {
	Label string     `json:"label,omitempty"`
	Steps []mathStep `json:"steps"`
}

func checkMath(raw json.RawMessage) (any, error) {
	var s mathSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := text("label", s.Label, 80, false); err != nil {
		return nil, err
	}
	if err := count("steps", len(s.Steps), 1, 20); err != nil {
		return nil, err
	}
	for _, st := range s.Steps {
		if err := text("expression", st.Expression, 240, true); err != nil {
			return nil, err
		}
		if err := text("note", st.Note, 160, false); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ---- spec sheet ----

type specRow struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Emphasis bool   `json:"emphasis,omitempty"`
}

type specSheet struct {
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle,omitempty"`
	Rows     []specRow `json:"rows"`
}

func checkSpecSheet(raw json.RawMessage) (any, error) {
	var s specSheet
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := text("title", s.Title, 80, true); err != nil {
		return nil, err
	}
	if err := text("subtitle", s.Subtitle, 120, false); err != nil {
		return nil, err
	}
	if err := count("rows", len(s.Rows), 1, 30); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, r := range s.Rows {
		if err := text("label", r.Label, 40, true); err != nil {
			return nil, err
		}
		if seen[r.Label] {
			return nil, fmt.Errorf("duplicate row label %q", r.Label)
		}
		seen[r.Label] = true
		if err := text("value", r.Value, 160, false); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ---- comparison ----

type comparisonOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Headline string `json:"headline"`
	// Traits holds one entry per trait label: the value text, or null when the option lacks the trait.
	Traits []*string `json:"traits"`
}

type comparisonSpec struct {
	TraitLabels   []string           `json:"traitLabels"`
	Options       []comparisonOption `json:"options"`
	RecommendedID string             `json:"recommendedId"`
	Reason        string             `json:"reason"`
}

func checkComparison(raw json.RawMessage) (any, error) {
	var s comparisonSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := count("traitLabels", len(s.TraitLabels), 1, 10); err != nil {
		return nil, err
	}
	if err := texts("trait label", s.TraitLabels, 40); err != nil {
		return nil, err
	}
	if err := count("options", len(s.Options), 2, 4); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, o := range s.Options {
		if err := text("option id", o.ID, 40, true); err != nil {
			return nil, err
		}
		if ids[o.ID] {
			return nil, fmt.Errorf("duplicate option id %q", o.ID)
		}
		ids[o.ID] = true
		if err := text("option name", o.Name, 40, true); err != nil {
			return nil, err
		}
		if err := text("headline", o.Headline, 80, false); err != nil {
			return nil, err
		}
		if len(o.Traits) != len(s.TraitLabels) {
			return nil, fmt.Errorf("option %q needs exactly %d traits (null where it lacks one)", o.ID, len(s.TraitLabels))
		}
		for _, t := range o.Traits {
			if t != nil {
				if err := text("trait", *t, 60, false); err != nil {
					return nil, err
				}
			}
		}
	}
	if !ids[s.RecommendedID] {
		return nil, fmt.Errorf("recommendedId must be one of the option ids")
	}
	if err := text("reason", s.Reason, 240, true); err != nil {
		return nil, err
	}
	return s, nil
}

// ---- timeline ----

type timelineEvent struct {
	ID     string `json:"id"`
	When   string `json:"when"`
	Time   string `json:"time"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type timelineSpec struct {
	Title  string          `json:"title,omitempty"`
	Events []timelineEvent `json:"events"`
}

// timelineInput also takes the names models reach for first (date, state, description), so a
// reasonable call is not rejected over vocabulary; the stored spec always uses the canonical names.
type timelineInput struct {
	Title  string `json:"title"`
	Events []struct {
		ID          string `json:"id"`
		When        string `json:"when"`
		State       string `json:"state"`
		Time        string `json:"time"`
		Date        string `json:"date"`
		Title       string `json:"title"`
		Detail      string `json:"detail"`
		Description string `json:"description"`
	} `json:"events"`
}

func checkTimeline(raw json.RawMessage) (any, error) {
	var in timelineInput
	if err := decode(raw, &in); err != nil {
		return nil, err
	}
	if err := text("title", in.Title, 80, false); err != nil {
		return nil, err
	}
	if err := count("events", len(in.Events), 1, 30); err != nil {
		return nil, err
	}
	s := timelineSpec{Title: in.Title}
	ids := map[string]bool{}
	for i, e := range in.Events {
		ev := timelineEvent{ID: e.ID, When: cmp.Or(e.When, e.State), Time: cmp.Or(e.Time, e.Date), Title: e.Title, Detail: cmp.Or(e.Detail, e.Description)}
		if ev.ID == "" {
			ev.ID = fmt.Sprintf("e%d", i+1)
		}
		if err := text("event id", ev.ID, 40, true); err != nil {
			return nil, err
		}
		if ids[ev.ID] {
			return nil, fmt.Errorf("duplicate event id %q", ev.ID)
		}
		ids[ev.ID] = true
		if ev.When != "past" && ev.When != "now" && ev.When != "future" {
			return nil, fmt.Errorf("event %d: when must be past, now or future", i+1)
		}
		if err := text("time", ev.Time, 16, true); err != nil {
			return nil, err
		}
		if err := text("title", ev.Title, 80, true); err != nil {
			return nil, err
		}
		if err := text("detail", ev.Detail, 200, false); err != nil {
			return nil, err
		}
		s.Events = append(s.Events, ev)
	}
	return s, nil
}

// ---- job progress ----

type jobStage struct {
	Name        string  `json:"name"`
	Weight      float64 `json:"weight"`
	Description string  `json:"description,omitempty"`
}

type jobOutcome struct {
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type jobSpec struct {
	Title         string      `json:"title"`
	Stages        []jobStage  `json:"stages"`
	StageIndex    int         `json:"stageIndex"`
	StageProgress float64     `json:"stageProgress"`
	ETA           string      `json:"eta,omitempty"`
	ElapsedMs     float64     `json:"elapsedMs,omitempty"`
	Outcome       *jobOutcome `json:"outcome,omitempty"`
}

func checkJob(raw json.RawMessage) (any, error) {
	var s jobSpec
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	if err := text("title", s.Title, 80, true); err != nil {
		return nil, err
	}
	if err := count("stages", len(s.Stages), 1, 12); err != nil {
		return nil, err
	}
	for _, st := range s.Stages {
		if err := text("stage name", st.Name, 40, true); err != nil {
			return nil, err
		}
		if err := text("stage description", st.Description, 120, false); err != nil {
			return nil, err
		}
		if !(st.Weight > 0) || st.Weight > 1000 {
			return nil, fmt.Errorf("stage weights are positive numbers")
		}
	}
	if s.StageIndex < 0 || s.StageIndex > len(s.Stages) || s.StageProgress < 0 || s.StageProgress > 1 {
		return nil, fmt.Errorf("stageIndex is 0-%d and stageProgress 0-1", len(s.Stages))
	}
	if err := text("eta", s.ETA, 30, false); err != nil {
		return nil, err
	}
	if s.ElapsedMs < 0 {
		return nil, fmt.Errorf("elapsedMs cannot be negative")
	}
	if o := s.Outcome; o != nil {
		switch o.Status {
		case "success", "partial", "failed", "cancelled":
		default:
			return nil, fmt.Errorf("outcome status must be success, partial, failed or cancelled")
		}
		if err := text("outcome summary", o.Summary, 240, false); err != nil {
			return nil, err
		}
	}
	return s, nil
}

const strSchema = `{"type":"string"}`

var showTools = []showTool{
	{ShowTable, "Show a sortable table. Use for lists of items with several attributes. Values are strings, numbers, booleans or short lists; give numbers as numbers and set a column format (number, currency, percent, delta, date, badge, list) so they render properly. Dates are ISO 8601. Mark the main column priority=primary.",
		`{"type":"object","properties":{"caption":{"type":"string"},"columns":{"type":"array","minItems":1,"maxItems":12,"items":{"type":"object","properties":{"key":{"type":"string"},"label":{"type":"string"},"priority":{"type":"string","enum":["primary","secondary"]},"format":{"type":"object","properties":{"kind":{"type":"string","enum":["text","number","currency","percent","delta","date","boolean","badge","list"]},"decimals":{"type":"integer"},"compact":{"type":"boolean"},"unit":{"type":"string"},"currency":{"type":"string","description":"ISO 4217 code, for kind=currency"},"basis":{"type":"string","enum":["fraction","unit"],"description":"percent: 0.12 is fraction, 12 is unit"},"style":{"type":"string","enum":["date","datetime","relative"]},"max":{"type":"integer"}},"required":["kind"]}},"required":["key","label"]}},"rows":{"type":"array","maxItems":100,"items":{"type":"object","description":"One value per column key"}}},"required":["columns","rows"]}`, checkTable},
	{ShowChart, "Show one numeric series as a small chart with a headline value. Use for a trend or distribution over time or categories.",
		`{"type":"object","properties":{"label":{"type":"string"},"value":{"type":"string","description":"Headline number, already formatted"},"delta":{"type":"string","description":"Optional change, e.g. +12%"},"trend":{"type":"string","enum":["up","down","flat"]},"upIsGood":{"type":"boolean","description":"false when a rise is bad news (latency, cost)"},"points":{"type":"array","minItems":1,"maxItems":200,"items":{"type":"number"}},"variant":{"type":"string","enum":["area","line","bars"]}},"required":["label","value","points"]}`, checkChart},
	{ShowDiagram, "Show a diagram of labelled boxes joined by arrows on a 600x400 canvas, e.g. an architecture or relationship sketch. x,y are the top-left corner of a node; lay nodes out so they do not overlap.",
		`{"type":"object","properties":{"title":{"type":"string"},"nodes":{"type":"array","minItems":1,"maxItems":40,"items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"x":{"type":"number"},"y":{"type":"number"},"width":{"type":"number","description":"default 120"},"height":{"type":"number","description":"default 44"},"shape":{"type":"string","enum":["box","round","pill"]}},"required":["id","label","x","y"]}},"links":{"type":"array","maxItems":80,"items":{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"},"label":{"type":"string"}},"required":["from","to"]}}},"required":["title","nodes","links"]}`, checkDiagram},
	{ShowFlow, "Show a flow of steps laid out on a grid (column = left to right, row = top to bottom) with a state per step: done, active or pending. Use for pipelines and processes.",
		`{"type":"object","properties":{"nodes":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string","description":"Short, 24 characters at most"},"column":{"type":"integer","minimum":0,"maximum":9},"row":{"type":"integer","minimum":0,"maximum":7},"state":{"type":"string","enum":["done","active","pending"]}},"required":["id","label","column","row","state"]}},"edges":{"type":"array","maxItems":60,"items":{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}}},"required":["nodes","edges"]}`, checkFlow},
	{ShowMath, "Show the steps of a derivation or calculation, each an expression with an optional note. Use plain Unicode maths, not LaTeX.",
		`{"type":"object","properties":{"label":{"type":"string"},"steps":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{"expression":{"type":"string"},"note":{"type":"string"}},"required":["expression"]}}},"required":["steps"]}`, checkMath},
	{ShowSpecSheet, "Show a card of labelled facts about one thing (a product, a machine, a person). Set emphasis on the key rows.",
		`{"type":"object","properties":{"title":{"type":"string"},"subtitle":{"type":"string"},"rows":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","properties":{"label":{"type":"string"},"value":{"type":"string"},"emphasis":{"type":"boolean"}},"required":["label","value"]}}},"required":["title","rows"]}`, checkSpecSheet},
	{ShowComparison, "Show 2-4 options side by side against shared traits, with one recommendation and the reason. Each option has one trait entry per traitLabel: the text, or null when the option lacks it.",
		`{"type":"object","properties":{"traitLabels":{"type":"array","minItems":1,"maxItems":10,"items":` + strSchema + `},"options":{"type":"array","minItems":2,"maxItems":4,"items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"headline":{"type":"string"},"traits":{"type":"array","items":{"type":["string","null"]}}},"required":["id","name","headline","traits"]}},"recommendedId":{"type":"string"},"reason":{"type":"string"}},"required":["traitLabels","options","recommendedId","reason"]}`, checkComparison},
	{ShowTimeline, "Show events in order, each with when (past, now or future), a short time label (09:30, Mar 4, 2026-01-20), a title and an optional detail. An optional top-level title names the timeline. Example: {\"title\":\"Launch\",\"events\":[{\"when\":\"past\",\"time\":\"Mar 4\",\"title\":\"Design freeze\",\"detail\":\"Specs locked\"},{\"when\":\"now\",\"time\":\"Today\",\"title\":\"Announcement\"}]}",
		`{"type":"object","properties":{"title":{"type":"string"},"events":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","properties":{"id":{"type":"string","description":"optional"},"when":{"type":"string","enum":["past","now","future"]},"time":{"type":"string"},"title":{"type":"string"},"detail":{"type":"string"}},"required":["when","time","title"]}}},"required":["events"]}`, checkTimeline},
	{ShowJob, "Show the progress of staged work. Stages have relative weights; stageIndex is the stage in progress (equal to the number of stages when all are done) and stageProgress its 0-1 completion. Set outcome when the work has ended.",
		`{"type":"object","properties":{"title":{"type":"string"},"stages":{"type":"array","minItems":1,"maxItems":12,"items":{"type":"object","properties":{"name":{"type":"string"},"weight":{"type":"number"},"description":{"type":"string"}},"required":["name","weight"]}},"stageIndex":{"type":"integer","minimum":0},"stageProgress":{"type":"number","minimum":0,"maximum":1},"eta":{"type":"string"},"elapsedMs":{"type":"number"},"outcome":{"type":"object","properties":{"status":{"type":"string","enum":["success","partial","failed","cancelled"]},"summary":{"type":"string"}},"required":["status"]}},"required":["title","stages","stageIndex","stageProgress"]}`, checkJob},
}

var _ chat.Tool = Show{}
