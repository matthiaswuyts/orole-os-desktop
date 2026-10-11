package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

func skillSource(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n# " + name + "\nInstructions for " + name + ".\n"
}

// skillFixture holds three skills (two enabled, one disabled) with a resource file in the first.
func skillFixture(t *testing.T) (*skills.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := skills.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct{ name, desc string }{{"alpha", "Does alpha things"}, {"beta", "Does beta things"}, {"gamma", "Disabled"}} {
		if _, err := store.Save("", skillSource(s.name, s.desc), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SetEnabled("gamma", false); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills", "alpha", "ref"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "alpha", "ref", "notes.txt"), []byte("alpha notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outside-secret.txt"), []byte("TOP SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	return store, dir
}

func skillsBot(names ...string) chat.Bot {
	var b chat.Bot
	b.ID = "b1"
	b.Config.Skills = names
	return b
}

func callSkill(t *testing.T, tool *SkillsTool, bot chat.Bot, args string) chat.ToolResult {
	t.Helper()
	res, err := tool.Call(context.Background(), chat.RunContext{BotID: bot.ID, Bot: bot, Actor: "bot:" + bot.ID}, GetSkillName, json.RawMessage(args))
	if err != nil {
		t.Fatalf("Call(%s) = %v", args, err)
	}
	return res
}

func TestSkillsSpecs(t *testing.T) {
	store, _ := skillFixture(t)
	tool := &SkillsTool{Store: store}
	specs := tool.Specs(skillsBot("alpha"), llm.Caps{Tools: true})
	if len(specs) != 1 || specs[0].Name != GetSkillName {
		t.Fatalf("specs = %v", specNames(specs))
	}
	if got := tool.Specs(skillsBot(), llm.Caps{Tools: true}); got != nil {
		t.Fatalf("no skills enabled: specs = %v", got)
	}
	if got := tool.Specs(skillsBot("alpha"), llm.Caps{}); got != nil {
		t.Fatalf("model without tools: specs = %v", got)
	}
	if got := (&SkillsTool{}).Specs(skillsBot("alpha"), llm.Caps{Tools: true}); got != nil {
		t.Fatalf("no store: specs = %v", got)
	}
}

func TestSkillsReadOnlyBotListedEnabledSkills(t *testing.T) {
	store, _ := skillFixture(t)
	tool := &SkillsTool{Store: store}
	bot := skillsBot("alpha", "gamma", "missing")

	res := callSkill(t, tool, bot, `{"name":"alpha"}`)
	if res.IsError || !strings.Contains(res.Text, "Instructions for alpha.") || !strings.Contains(res.Text, "ref/notes.txt") {
		t.Fatalf("alpha = %+v", res)
	}
	if res.Untrusted {
		t.Error("installed skill text is the user's instructions, not untrusted content")
	}
	for _, args := range []string{
		`{"name":"beta"}`,    // enabled in the registry but not in this bot's list
		`{"name":"gamma"}`,   // listed but disabled
		`{"name":"missing"}`, // listed but absent
		`{"name":""}`, `{}`, `{"name":7}`,
	} {
		if res := callSkill(t, tool, bot, args); !res.IsError {
			t.Errorf("%s: got %+v, want an error result", args, res)
		}
	}
	if res := callSkill(t, tool, bot, `not json`); !res.IsError {
		t.Error("malformed arguments must be an error result")
	}
}

func TestSkillsFileConfinement(t *testing.T) {
	store, dir := skillFixture(t)
	tool := &SkillsTool{Store: store}
	bot := skillsBot("alpha")

	if res := callSkill(t, tool, bot, `{"name":"alpha","file":"ref/notes.txt"}`); res.IsError || res.Text != "alpha notes" {
		t.Fatalf("resource file = %+v", res)
	}
	if res := callSkill(t, tool, bot, `{"name":"alpha","file":"SKILL.md"}`); res.IsError || !strings.Contains(res.Text, "Instructions for alpha.") {
		t.Fatalf("SKILL.md = %+v", res)
	}
	abs := filepath.Join(dir, "outside-secret.txt")
	for _, file := range []string{
		"../outside-secret.txt",
		"../../outside-secret.txt",
		"../beta/SKILL.md",
		"ref/../../beta/SKILL.md",
		`..\outside-secret.txt`,
		abs,
		"/etc/passwd",
		"nope.txt",
	} {
		args, _ := json.Marshal(map[string]string{"name": "alpha", "file": file})
		res := callSkill(t, tool, bot, string(args))
		if !res.IsError {
			t.Errorf("file %q: got %+v, want an error result", file, res)
		}
		if strings.Contains(res.Text, "TOP SECRET") || strings.Contains(res.Text, "Instructions for beta") {
			t.Errorf("file %q leaked content: %q", file, res.Text)
		}
	}
}

func TestSkillsPromptRestrictedToBotList(t *testing.T) {
	store, _ := skillFixture(t)
	got := SkillsPrompt(store, skillsBot("alpha", "gamma", "missing"))
	if !strings.Contains(got, "- alpha: Does alpha things") {
		t.Fatalf("prompt = %q", got)
	}
	for _, bad := range []string{"beta", "gamma", "missing"} {
		if strings.Contains(got, bad) {
			t.Errorf("prompt lists %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, GetSkillName) {
		t.Errorf("prompt does not tell the model how to read a skill: %q", got)
	}
	if got := SkillsPrompt(store, skillsBot()); got != "" {
		t.Errorf("bot without skills: prompt = %q", got)
	}
	if got := SkillsPrompt(nil, skillsBot("alpha")); got != "" {
		t.Errorf("nil store: prompt = %q", got)
	}
	if got := SkillsPrompt(store, skillsBot("missing", "gamma")); got != "" {
		t.Errorf("nothing enabled: prompt = %q", got)
	}
}

func TestSkillsPromptFlattensDescriptions(t *testing.T) {
	dir := t.TempDir()
	store, err := skills.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save("", "---\nname: wide\ndescription: \"line one\\nline two\"\n---\nBody\n", ""); err != nil {
		t.Fatal(err)
	}
	got := SkillsPrompt(store, skillsBot("wide"))
	if strings.Count(got, "- wide:") != 1 || strings.Contains(strings.SplitN(got, "- wide:", 2)[1], "\n\n") {
		t.Fatalf("prompt = %q", got)
	}
}

// builtinFixture is the fixture plus the built-in skills, as the daemon installs them.
func builtinFixture(t *testing.T) *skills.Store {
	t.Helper()
	store, _ := skillFixture(t)
	if err := store.EnsureBuiltin(); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestBuiltinSkillsReachEveryBot(t *testing.T) {
	store := builtinFixture(t)
	tool := &SkillsTool{Store: store}
	bot := skillsBot() // no skills configured

	if specs := tool.Specs(bot, llm.Caps{Tools: true}); len(specs) != 1 {
		t.Fatalf("a bot with no skills must still get the reader for built-in skills: %v", specNames(specs))
	}
	if got := SkillsPrompt(store, bot); !strings.Contains(got, "- skill-creator:") || strings.Contains(got, "alpha") {
		t.Fatalf("prompt = %q", got)
	}
	res := callSkill(t, tool, bot, `{"name":"skill-creator"}`)
	if res.IsError || !strings.Contains(res.Text, "propose_skill") || !strings.Contains(res.Text, "references/examples.md") {
		t.Fatalf("skill-creator = %+v", res)
	}
	if res := callSkill(t, tool, bot, `{"name":"skill-creator","file":"references/examples.md"}`); res.IsError {
		t.Fatalf("resource file = %+v", res)
	}
	// Built-in access does not widen into the rest of the registry.
	for _, name := range []string{"alpha", "missing"} {
		if res := callSkill(t, tool, bot, `{"name":"`+name+`"}`); !res.IsError {
			t.Errorf("%s was readable without a grant", name)
		}
	}
}

func TestDisabledBuiltinSkillIsNeitherOfferedNorReadable(t *testing.T) {
	store := builtinFixture(t)
	if _, err := store.SetEnabled("skill-creator", false); err != nil {
		t.Fatal(err)
	}
	tool := &SkillsTool{Store: store}
	bot := skillsBot()
	if specs := tool.Specs(bot, llm.Caps{Tools: true}); specs != nil {
		t.Fatalf("specs = %v", specNames(specs))
	}
	if got := SkillsPrompt(store, bot); got != "" {
		t.Fatalf("prompt = %q", got)
	}
	if res := callSkill(t, tool, bot, `{"name":"skill-creator"}`); !res.IsError {
		t.Fatalf("disabled built-in skill was served: %+v", res)
	}
}
