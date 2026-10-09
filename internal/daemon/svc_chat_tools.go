package daemon

import (
	"context"
	"slices"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
)

// registerChatTools runs after registerChat and registerChatMemory: it adds the web, research, MCP and
// skills tools to the chat Manager and answers chat.tools.list. knowledge_search is listed only when
// the memory subsystem registered it, which is why this registrar comes last.
func (d *Daemon) registerChatTools() {
	m := d.Chat
	if m == nil {
		return
	}

	fetcher := &tools.Fetcher{}
	if d.Policy != nil {
		fetcher.Policy = d.Policy
	}
	research := &tools.Research{LLM: d.LLM, Fetcher: fetcher}
	list := []chat.Tool{
		tools.WebSearch{},
		tools.WebFetch{Fetcher: fetcher},
		tools.Datetime{},
		research,
	}
	if d.MCP != nil {
		list = append(list, &tools.MCPTools{Client: d.MCP})
	}
	if d.Skills != nil {
		list = append(list, &tools.SkillsTool{Store: d.Skills})
		m.AddPromptSection(func(b chat.Bot) string { return tools.SkillsPrompt(d.Skills, b) })
	}
	m.AddTools(list...)

	info := tools.InfoSource{
		LLM:       d.LLM,
		ModelCaps: m.ModelCaps,
		HasTool: func(id string) bool {
			return slices.ContainsFunc(m.Tools(), func(t chat.Tool) bool { return t.ID() == id })
		},
		Fetcher: fetcher,
		Native:  research.Native,
	}
	handle(d, "chat.tools.list", func(ctx context.Context, p chat.ToolsRequest) ([]chat.ToolInfo, error) {
		out, err := info.Describe(ctx, p)
		return out, chatError(err)
	})
}
