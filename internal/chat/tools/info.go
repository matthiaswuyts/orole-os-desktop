package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// InfoSource is what Describe needs from the daemon.
type InfoSource struct {
	// LLM supplies defaults and provider display names.
	LLM interface {
		Statuses(ctx context.Context) ([]llm.ProviderStatus, error)
		Defaults(ctx context.Context) (llm.Defaults, error)
	}
	// ModelCaps resolves the capabilities of a model (chat.Manager.ModelCaps).
	ModelCaps func(ctx context.Context, provider, model string) (llm.Caps, error)
	// HasTool reports whether a client tool with that id is registered (used for knowledge_search,
	// which the memory subsystem provides). Nil means "none".
	HasTool func(id string) bool
	// Fetcher is non-nil when the fetch_url fallback exists.
	Fetcher *Fetcher
	// Native is the optional native research path.
	Native Native
}

// Describe answers chat.tools.list: which toggleable tools the provider/model can run, with the
// reason a tool is unavailable and what leaves the machine when it runs.
func (s InfoSource) Describe(ctx context.Context, req chat.ToolsRequest) ([]chat.ToolInfo, error) {
	providerID, model := req.Provider, req.Model
	if providerID == "" || model == "" {
		d, err := s.LLM.Defaults(ctx)
		if err != nil {
			return nil, err
		}
		if pid, m, ok := llm.SplitModel(d.ChatModel); ok {
			if providerID == "" {
				providerID = pid
			}
			if model == "" {
				model = m
			}
		}
	}
	infos := baseInfos()
	if providerID == "" || model == "" {
		return s.keep(withReason(infos, "Choose a model first.")), nil
	}
	name := providerID
	if statuses, err := s.LLM.Statuses(ctx); err == nil {
		for _, st := range statuses {
			if st.ID == providerID && st.Name != "" {
				name = untrusted.Line(st.Name, 60)
			}
		}
	}
	caps, err := s.ModelCaps(ctx, providerID, model)
	if err != nil {
		reason := "This model's capabilities are unknown."
		switch {
		case errors.Is(err, llm.ErrNotConfigured):
			reason = fmt.Sprintf("Connect %s first.", name)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		}
		return s.keep(withReason(infos, reason)), nil
	}

	search := hasHosted(caps, llm.HostedWebSearch)
	fetch := hasHosted(caps, llm.HostedWebFetch)
	byID := map[string]*chat.ToolInfo{}
	for i := range infos {
		byID[infos[i].ID] = &infos[i]
	}

	ws := byID[chat.ToolWebSearch]
	ws.Available = search
	if search {
		ws.Egress = "Search queries are sent to " + name + "."
	} else {
		ws.Reason = "This model's endpoint cannot run hosted web search."
	}

	wf := byID[chat.ToolWebFetch]
	switch {
	case fetch:
		wf.Available = true
		wf.Egress = "Fetched URLs are requested by " + name + "."
	case caps.Tools && s.Fetcher != nil:
		wf.Available = true
		wf.Egress = "Fetched URLs are requested directly from this computer."
	case !caps.Tools:
		wf.Reason = "This model cannot call tools, so it cannot fetch pages."
	default:
		wf.Reason = "This model's endpoint cannot fetch pages."
	}

	dt := byID[chat.ToolDatetime]
	switch {
	case hasHosted(caps, llm.HostedDatetime):
		dt.Available = true
		dt.Egress = "Nothing leaves this computer."
	case caps.Tools:
		dt.Available = true
		dt.Egress = "Nothing leaves this computer."
	default:
		dt.Reason = "This model cannot call tools."
	}

	rs := byID[chat.ToolResearch]
	native := s.Native != nil && caps.DeepResearch && s.Native.Supports(providerID, caps)
	switch {
	case native:
		rs.Available = true
		rs.Egress = "The question and everything the research reads are sent to " + name + "."
	case !caps.Tools:
		rs.Reason = "Research needs a model that can call tools."
	case !search:
		rs.Reason = "Research needs web search, which this model's endpoint cannot run."
	default:
		rs.Available = true
		rs.Egress = "The question and search queries are sent to " + name + "; pages are read by " + name + " or fetched from this computer."
	}

	kn := byID[chat.ToolKnowledge]
	kn.Available = caps.Tools
	if caps.Tools {
		kn.Egress = "Passages found are sent to " + name + " as part of the conversation."
	} else {
		kn.Reason = "This model cannot call tools."
	}
	return s.keep(infos), nil
}

// keep drops knowledge_search unless the memory subsystem registered it.
func (s InfoSource) keep(infos []chat.ToolInfo) []chat.ToolInfo {
	if s.HasTool != nil && s.HasTool(chat.ToolKnowledge) {
		return infos
	}
	return slices.DeleteFunc(infos, func(i chat.ToolInfo) bool { return i.ID == chat.ToolKnowledge })
}

// withReason marks every tool unavailable for one common reason.
func withReason(infos []chat.ToolInfo, reason string) []chat.ToolInfo {
	for i := range infos {
		infos[i].Reason = reason
	}
	return infos
}

func baseInfos() []chat.ToolInfo {
	return []chat.ToolInfo{
		{ID: chat.ToolWebSearch, Name: "Web search", Description: "Search the web for current information."},
		{ID: chat.ToolWebFetch, Name: "Web pages", Description: "Open and read web pages and links."},
		{ID: chat.ToolDatetime, Name: "Date and time", Description: "Know the current date and time."},
		{ID: chat.ToolResearch, Name: "Research", Description: "Plan, search, read and write a cited report. Slow and uses many model calls."},
		{ID: chat.ToolKnowledge, Name: "Knowledge search", Description: "Search the knowledge you shared with this bot."},
	}
}
