package agents

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	EvUsage          = "usage"
	EvQuestion       = "question"
	EvQuestionCancel = "question_cancel"
	EvAnswer         = "answer"
	EvSources        = "sources"
	EvFile           = "file"
	EvImageStatus    = "image_status"
	EvSubagent       = "subagent"
	EvGuardrail      = "guardrail"
)

// ContextUsage contains provider observations, never a guessed model capacity.
// ContextTokens is current occupancy, not the cumulative billed token count.
type ContextUsage struct {
	Source        string    `json:"source"`
	InputTokens   *int64    `json:"inputTokens,omitempty"`
	OutputTokens  *int64    `json:"outputTokens,omitempty"`
	CachedTokens  *int64    `json:"cachedTokens,omitempty"`
	ContextTokens *int64    `json:"contextTokens,omitempty"`
	ContextWindow *int64    `json:"contextWindow,omitempty"`
	At            time.Time `json:"at"`
	// Breakdown is the harness's own per-category accounting of the window. It is nil when the
	// harness reports totals only; Orole never derives one.
	Breakdown *ContextBreakdown `json:"breakdown,omitempty"`
}

// ContextBreakdown is a harness-reported split of the context window (Claude Code's
// get_context_usage). It is immutable once emitted; a newer report replaces it whole.
type ContextBreakdown struct {
	Model        string `json:"model,omitempty"`
	UsedTokens   int64  `json:"usedTokens"`
	WindowTokens int64  `json:"windowTokens"`
	// AutoCompactTokens is the occupancy at which the harness compacts; nil when auto-compact is off or unreported.
	AutoCompactTokens *int64            `json:"autoCompactTokens,omitempty"`
	Categories        []ContextCategory `json:"categories"`
	At                time.Time         `json:"at"`
}

// ContextCategory is one row of a breakdown. Kind is "used" (occupies the window), "free"
// (remaining), "buffer" (compaction reserve) or "deferred" (listed, but outside the window).
type ContextCategory struct {
	Name   string        `json:"name"`
	Tokens int64         `json:"tokens"`
	Kind   string        `json:"kind"`
	Items  []ContextItem `json:"items,omitempty"`
}

// ContextItem is one contributor inside a category (a memory file, an MCP server, a skill...).
type ContextItem struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Tokens int64  `json:"tokens"`
}

type QuestionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type Question struct {
	ID        string           `json:"id"`
	Header    string           `json:"header"`
	Prompt    string           `json:"prompt"`
	Options   []QuestionOption `json:"options"`
	Multiple  bool             `json:"multiple"`
	AllowText bool             `json:"allowText"`
	Secret    bool             `json:"secret"`
}

type QuestionRequest struct {
	ID        string     `json:"id"`
	Questions []Question `json:"questions"`
	At        time.Time  `json:"at"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Values     []string `json:"values"`
}

// InputResponder is optional: a permission Boolean must never answer a question.
type InputResponder interface {
	AnswerInput(requestID string, answers []QuestionAnswer, cancel bool) error
}

type SourceReference struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type SourcesData struct {
	ID    string            `json:"id"`
	Items []SourceReference `json:"items"`
}

type FileData struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type ImageStatusData struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Prompt string `json:"prompt"`
	Error  string `json:"error"`
}

type SubagentData struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Task       string `json:"task"`
	Message    string `json:"message"`
	ToolCallID string `json:"toolCallId"`
}

type GuardrailData struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

// Adapter validation is independent of daemon validation: Process users cannot
// change the immutable wire request, answer a stale snapshot, or smuggle choices.
func adapterValidateAnswers(req QuestionRequest, answers []QuestionAnswer, cancel bool) error {
	if req.ExpiresAt != nil && !time.Now().Before(*req.ExpiresAt) {
		return fmt.Errorf("question request has expired")
	}
	if cancel {
		if len(answers) != 0 {
			return fmt.Errorf("a cancellation cannot contain answers")
		}
		return nil
	}
	if len(answers) != len(req.Questions) {
		return fmt.Errorf("answer every question exactly once")
	}
	seen := make(map[string]bool, len(answers))
	total := 0
	for _, a := range answers {
		if seen[a.QuestionID] {
			return fmt.Errorf("duplicate question answer %q", a.QuestionID)
		}
		seen[a.QuestionID] = true
		var q *Question
		for i := range req.Questions {
			if req.Questions[i].ID == a.QuestionID {
				q = &req.Questions[i]
				break
			}
		}
		if q == nil || len(a.Values) == 0 || len(a.Values) > 64 || (!q.Multiple && len(a.Values) != 1) {
			return fmt.Errorf("invalid answer for question %q", a.QuestionID)
		}
		values := make(map[string]bool, len(a.Values))
		for _, value := range a.Values {
			total += len(value)
			if total > 64<<10 || len(value) > 8192 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.TrimSpace(value) == "" || values[value] {
				return fmt.Errorf("empty, duplicate, or oversized answer")
			}
			values[value] = true
			allowed := q.AllowText
			for _, option := range q.Options {
				allowed = allowed || option.ID == value
			}
			if !allowed {
				return fmt.Errorf("answer is not an offered option for %q", a.QuestionID)
			}
		}
	}
	if len(jraw(answers)) > 64<<10 {
		return fmt.Errorf("answer payload is oversized")
	}
	return nil
}

func adapterValidQuestions(questions []Question) bool {
	if len(questions) == 0 || len(questions) > 32 {
		return false
	}
	seen := make(map[string]bool, len(questions))
	for _, q := range questions {
		if q.ID == "" || len(q.ID) > 256 || !utf8.ValidString(q.ID) || strings.ContainsRune(q.ID, 0) || seen[q.ID] || strings.TrimSpace(q.Prompt) == "" || len(q.Options) > 64 || (!q.AllowText && len(q.Options) == 0) {
			return false
		}
		seen[q.ID] = true
		opts := make(map[string]bool, len(q.Options))
		for _, o := range q.Options {
			if strings.TrimSpace(o.ID) == "" || len(o.ID) > 8192 || !utf8.ValidString(o.ID) || strings.ContainsRune(o.ID, 0) || strings.TrimSpace(o.Label) == "" || opts[o.ID] {
				return false
			}
			opts[o.ID] = true
		}
	}
	return true
}

func observedTokens(v *int64) *int64 {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

func usageEvent(u ContextUsage) []Event {
	u.InputTokens, u.OutputTokens, u.CachedTokens = observedTokens(u.InputTokens), observedTokens(u.OutputTokens), observedTokens(u.CachedTokens)
	u.ContextTokens, u.ContextWindow = observedTokens(u.ContextTokens), observedTokens(u.ContextWindow)
	if u.InputTokens == nil && u.OutputTokens == nil && u.CachedTokens == nil && u.ContextTokens == nil && u.ContextWindow == nil && u.Breakdown == nil {
		return nil
	}
	u.At = time.Now().UTC()
	return []Event{{Kind: EvUsage, At: u.At, Data: jraw(u)}}
}
