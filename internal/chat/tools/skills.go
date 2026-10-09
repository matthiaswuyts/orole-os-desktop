package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/skills"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// GetSkillName is the function name of the skill reader; it matches the gateway's tool.
const GetSkillName = "orole_get_skill"

// SkillStore is the part of *skills.Store the tools use.
type SkillStore interface {
	Get(name string) (skills.Skill, error)
	ReadFile(name, rel string) ([]byte, error)
	Enabled() ([]skills.Skill, error)
}

// SkillsTool lets a bot read the enabled registry skills listed in BotConfig.Skills, and nothing
// else. Skill text is installed by the user, so it is returned as instructions (not Untrusted), the
// same way the MCP gateway serves it to coding agents.
type SkillsTool struct {
	Store SkillStore
}

func (*SkillsTool) ID() string { return GetSkillName }

func (t *SkillsTool) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if t.Store == nil || !caps.Tools || len(bot.Config.Skills) == 0 {
		return nil
	}
	return []llm.ToolSpec{{
		Name:        GetSkillName,
		Description: "Read the instructions of one of your skills (listed in your system prompt), or one of its resource files. Follow the returned instructions when the task matches the skill.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"name":{"type":"string","description":"Skill name"},` +
			`"file":{"type":"string","description":"Optional resource file path inside the skill; default SKILL.md"}},` +
			`"required":["name"],"additionalProperties":false}`),
	}}
}

func (t *SkillsTool) Call(_ context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if name != GetSkillName || t.Store == nil {
		return errResult("unknown tool")
	}
	var a struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Name == "" {
		return errResult("invalid arguments: name is required")
	}
	if !slices.Contains(rc.Bot.Config.Skills, a.Name) {
		return errResult("this bot has no skill named %q", untrusted.Line(a.Name, 64))
	}
	sk, err := t.Store.Get(a.Name)
	if err != nil {
		return errResult("skill unavailable: %s", skillErr(err))
	}
	if !sk.Enabled {
		return errResult("skill %q is disabled", sk.Name)
	}
	if a.File == "" || a.File == "SKILL.md" {
		var b strings.Builder
		b.WriteString(sk.Body)
		if len(sk.Files) > 0 {
			fmt.Fprintf(&b, "\n\n---\nResource files of this skill (read one with %s {\"name\": %q, \"file\": <path>}):\n", GetSkillName, sk.Name)
			for _, f := range sk.Files {
				b.WriteString("- " + f + "\n")
			}
		}
		return chat.ToolResult{Text: b.String()}, nil
	}
	data, err := t.Store.ReadFile(sk.Name, a.File)
	if err != nil {
		return errResult("skill file unavailable: %s", skillErr(err))
	}
	return chat.ToolResult{Text: string(data)}, nil
}

func skillErr(err error) string {
	switch {
	case errors.Is(err, skills.ErrNotFound):
		return "not found"
	case errors.Is(err, skills.ErrInvalid):
		return untrusted.Line(err.Error(), 200)
	}
	return "could not be read"
}

// SkillsPrompt lists the bot's skills (name and one-line description) for the engine to append to the
// system prompt. Only enabled registry skills named in bot.Config.Skills appear; it is empty when
// there are none. The instructions themselves are read on demand with orole_get_skill.
func SkillsPrompt(store interface {
	Enabled() ([]skills.Skill, error)
}, bot chat.Bot) string {
	if store == nil || len(bot.Config.Skills) == 0 {
		return ""
	}
	list, err := store.Enabled()
	if err != nil {
		return ""
	}
	const maxLines, maxDesc = 40, 300
	var b strings.Builder
	n := 0
	for _, sk := range list {
		if !slices.Contains(bot.Config.Skills, sk.Name) {
			continue
		}
		if n == 0 {
			b.WriteString("## Skills\n\nWhen a task matches a skill below, call `" + GetSkillName + "` with its name and follow the returned instructions before you start.\n\n")
		}
		if n == maxLines {
			b.WriteString("- ... more skills are available; ask for them by name\n")
			break
		}
		n++
		desc := strings.Join(strings.Fields(untrusted.Line(sk.Description, maxDesc)), " ")
		b.WriteString("- " + sk.Name + ": " + desc + "\n")
	}
	return b.String()
}
