package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func TestKnowledgeWriteIsOfferedToEveryToolCapableBot(t *testing.T) {
	e := newEnv(t)
	a := e.bot("a", "Alpha")
	tool := e.tool(ToolKnowledgeWrite)
	if specs := tool.Specs(a, toolCaps); len(specs) != 1 || specs[0].Name != ToolKnowledgeWrite {
		t.Fatalf("specs = %+v, want one knowledge_write spec without any knowledge present", specs)
	}
	if specs := tool.Specs(a, llm.Caps{}); specs != nil {
		t.Fatalf("offered to a model that cannot call tools: %+v", specs)
	}
}

func TestKnowledgeWriteSaveReplaceRemove(t *testing.T) {
	e := newEnv(t)
	a := e.bot("a", "Alpha")
	b := e.bot("b", "Beta")
	write := func(rc *run, args map[string]any) chat.ToolResult { return e.call(ToolKnowledgeWrite, rc, args) }

	res := write(newRun(a), map[string]any{"action": "save", "title": "Deploy guide", "text": "To deploy, run the zeppelin script and wait for green."})
	if res.IsError || !strings.Contains(res.Text, "Deploy guide") {
		t.Fatalf("save = %+v", res)
	}
	if res := e.call(chat.ToolKnowledge, newRun(a), map[string]any{"query": "zeppelin"}); !strings.Contains(res.Text, "zeppelin script") {
		t.Fatalf("saved note not searchable by its author: %+v", res)
	}
	if res := e.call(chat.ToolKnowledge, newRun(b), map[string]any{"query": "zeppelin"}); strings.Contains(res.Text, "zeppelin script") {
		t.Fatalf("another bot read this bot's knowledge: %+v", res)
	}

	// Saving the same title replaces the old text instead of adding a second file.
	write(newRun(a), map[string]any{"action": "save", "title": "Deploy guide", "text": "Deploys now use the mongoose pipeline."})
	files, err := e.s.KnowledgeFiles(t.Context(), "a")
	if err != nil || len(files) != 1 {
		t.Fatalf("files after replace = %+v, %v", files, err)
	}
	if res := e.call(chat.ToolKnowledge, newRun(a), map[string]any{"query": "zeppelin"}); strings.Contains(res.Text, "zeppelin script") {
		t.Fatalf("replaced text is still searchable: %+v", res)
	}
	if res := e.call(chat.ToolKnowledge, newRun(a), map[string]any{"query": "mongoose"}); !strings.Contains(res.Text, "mongoose pipeline") {
		t.Fatalf("new text not searchable: %+v", res)
	}
	if res := write(newRun(a), map[string]any{"action": "list"}); !strings.Contains(res.Text, "Deploy guide.md") {
		t.Fatalf("list = %+v", res)
	}

	var sha string
	if err := e.db.QueryRow(`SELECT sha256 FROM chat_attachments WHERE bot_id = 'a'`).Scan(&sha); err != nil {
		t.Fatal(err)
	}
	if res := write(newRun(a), map[string]any{"action": "remove", "title": "Deploy guide"}); res.IsError {
		t.Fatalf("remove = %+v", res)
	}
	if files, _ := e.s.KnowledgeFiles(t.Context(), "a"); len(files) != 0 {
		t.Fatalf("files after remove = %+v", files)
	}
	var rows int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM chat_attachments WHERE bot_id = 'a'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("attachment rows after remove = %d, %v", rows, err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "chat", "attachments", sha)); !os.IsNotExist(err) {
		t.Fatalf("blob left behind after remove: %v", err)
	}
	if res := write(newRun(a), map[string]any{"action": "remove", "title": "Deploy guide"}); !res.IsError {
		t.Fatal("removing a missing note succeeded")
	}
}

func TestKnowledgeWriteRejectsBadInput(t *testing.T) {
	e := newEnv(t)
	a := e.bot("a", "Alpha")
	for name, args := range map[string]map[string]any{
		"no title":   {"action": "save", "text": "x"},
		"no text":    {"action": "save", "title": "t", "text": "  "},
		"too long":   {"action": "save", "title": "t", "text": strings.Repeat("a", maxNoteChars+1)},
		"bad action": {"action": "burn"},
	} {
		if res := e.call(ToolKnowledgeWrite, newRun(a), args); !res.IsError {
			t.Errorf("%s accepted: %+v", name, res)
		}
	}
}
