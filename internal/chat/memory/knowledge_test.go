package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/extract"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func words(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "w%d ", i)
	}
	return b.String()
}

func TestChunkText(t *testing.T) {
	if got, over := chunkText(""); len(got) != 0 || over {
		t.Errorf("empty = %v %v", got, over)
	}
	if got, _ := chunkText(" \n\t "); len(got) != 0 {
		t.Errorf("blank = %q", got)
	}
	if got, over := chunkText("short text"); len(got) != 1 || got[0] != "short text" || over {
		t.Errorf("short = %q", got)
	}
	if got, _ := chunkText("a\r\nb"); len(got) != 1 || got[0] != "a\nb" {
		t.Errorf("CRLF = %q", got)
	}

	text := words(2000) // ~12k characters
	chunks, over := chunkText(text)
	if over || len(chunks) < 10 {
		t.Fatalf("%d chunks, overflow %v", len(chunks), over)
	}
	seen := map[string]bool{}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > chunkSize || n == 0 {
			t.Fatalf("chunk %d has %d characters", i, n)
		}
		if c != strings.TrimSpace(c) {
			t.Errorf("chunk %d not trimmed", i)
		}
		for _, w := range strings.Fields(c) {
			seen[w] = true
		}
		if i > 0 {
			// Overlap: the start of this chunk lies inside the previous one, on a word boundary.
			head := strings.Fields(c)[0]
			if !strings.Contains(chunks[i-1], " "+head+" ") && !strings.HasSuffix(chunks[i-1], " "+head) {
				t.Errorf("chunk %d does not overlap the previous one (starts at %q)", i, head)
			}
		}
	}
	for i := range 2000 {
		if !seen[fmt.Sprintf("w%d", i)] {
			t.Fatalf("word w%d lost between chunks", i)
		}
	}
	// Overlap is bounded: roughly chunkOverlap characters repeat, never half a chunk.
	for i := 1; i < len(chunks); i++ {
		first := strings.Fields(chunks[i])[0]
		at := strings.LastIndex(chunks[i-1], " "+first+" ")
		if at < 0 {
			continue
		}
		if shared := len(chunks[i-1]) - at; shared > chunkOverlap+30 || shared < chunkOverlap/3 {
			t.Errorf("chunk %d shares %d characters with its predecessor", i, shared)
		}
	}

	// Multi-byte text is never cut inside a character, and an unbroken run still makes progress.
	for _, text := range []string{strings.Repeat("héllo wörld ✓ 日本語 ", 400), strings.Repeat("日", 5000), strings.Repeat("x", 5000)} {
		cs, _ := chunkText(text)
		if len(cs) < 3 {
			t.Errorf("only %d chunks", len(cs))
		}
		for _, c := range cs {
			if !utf8.ValidString(c) || utf8.RuneCountInString(c) > chunkSize {
				t.Fatalf("bad chunk of %d runes", utf8.RuneCountInString(c))
			}
		}
	}

	// The per-file cap.
	huge := strings.Repeat("lorem ipsum dolor ", 200_000) // 3.6 MB
	cs, over := chunkText(huge)
	if len(cs) != maxChunksPerFile || !over {
		t.Errorf("cap: %d chunks, overflow %v", len(cs), over)
	}
}

func TestKnowledgeAddSearchRemove(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.bot("a", "Alpha")
	b := e.bot("b", "Beta")
	tool := e.tool(chat.ToolKnowledge)
	if specs := tool.Specs(a, toolCaps); specs != nil {
		t.Fatal("offered without knowledge files")
	}

	doc := "# Handbook\n\nWelcome to the company.\n\n" +
		"The zeppelin policy: employees may book a zeppelin ride once per year.\n\n" +
		strings.Repeat("Filler paragraph about lunch breaks and parking spaces. ", 60) + "\n\n" +
		"Expense reports are due on the fifth of each month.\n"
	att := e.attachment("handbook.md", "text/markdown", []byte(doc))
	kf, err := e.s.AddKnowledge(ctx, "a", att)
	if err != nil {
		t.Fatal(err)
	}
	if kf.Status != "ready" || kf.Chunks < 2 || kf.ID != att || kf.Name != "handbook.md" || kf.BotID != "a" || kf.Error != "" {
		t.Fatalf("file = %+v", kf)
	}
	files, err := e.s.KnowledgeFiles(ctx, "a")
	if err != nil || len(files) != 1 || files[0] != kf {
		t.Fatalf("list = %+v, %v", files, err)
	}
	if other, _ := e.s.KnowledgeFiles(ctx, "b"); len(other) != 0 {
		t.Errorf("other bot lists %+v", other)
	}
	if n := e.count(`SELECT COUNT(*) FROM knowledge_chunks WHERE bot_id = 'a' AND attachment_id = ?`, att); n != kf.Chunks {
		t.Errorf("chunk rows = %d, file says %d", n, kf.Chunks)
	}

	if specs := tool.Specs(a, toolCaps); len(specs) != 1 || specs[0].Name != chat.ToolKnowledge {
		t.Fatalf("specs = %+v", specs)
	}
	if specs := tool.Specs(b, toolCaps); specs != nil {
		t.Error("offered to a bot without knowledge")
	}
	if specs := tool.Specs(a, llm.Caps{}); specs != nil {
		t.Error("offered to a model without tools")
	}

	ra := newRun(a)
	res := e.call(chat.ToolKnowledge, ra, map[string]any{"query": "zeppelin ride"})
	if res.IsError || !res.Untrusted {
		t.Fatalf("res = %+v", res)
	}
	contains(t, res.Text, "matching passages", "from handbook.md", "employees may book a zeppelin ride")
	if n := strings.Count(res.Text, "\n[") + 1; n > maxSearchChunks {
		t.Errorf("%d passages", n)
	}
	// Scoped to the calling bot.
	res = e.call(chat.ToolKnowledge, newRun(b), map[string]any{"query": "zeppelin"})
	excludes(t, res.Text, "zeppelin ride")
	contains(t, res.Text, "No matching passages")
	if res := e.call(chat.ToolKnowledge, ra, map[string]any{"query": "***"}); !res.IsError {
		t.Error("empty query accepted")
	}

	// Recall surfaces the passage as fenced knowledge.
	rec, _ := e.s.Recall(ctx, a, "can I book a zeppelin?")
	contains(t, rec, "<untrusted", "[knowledge: handbook.md]", "zeppelin")
	if rec, _ := e.s.Recall(ctx, b, "can I book a zeppelin?"); rec != "" {
		t.Errorf("other bot recalled: %s", rec)
	}

	// Removal drops the chunks, the FTS rows and the tool.
	if err := e.s.RemoveKnowledge(ctx, "b", att); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("removing another bot's file = %v", err)
	}
	if err := e.s.RemoveKnowledge(ctx, "a", att); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM knowledge_chunks`); n != 0 {
		t.Errorf("%d chunks left", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM knowledge_fts WHERE knowledge_fts MATCH 'zeppelin'`); n != 0 {
		t.Errorf("%d FTS rows left", n)
	}
	if files, _ := e.s.KnowledgeFiles(ctx, "a"); len(files) != 0 {
		t.Errorf("files = %+v", files)
	}
	if specs := tool.Specs(a, toolCaps); specs != nil {
		t.Error("still offered after removal")
	}
	if res := e.call(chat.ToolKnowledge, ra, map[string]any{"query": "zeppelin"}); !strings.Contains(res.Text, "No matching") {
		t.Errorf("res = %+v", res)
	}
	if err := e.s.RemoveKnowledge(ctx, "a", att); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("second removal = %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM chat_attachments WHERE id = ?`, att); n != 1 {
		t.Error("the attachment row belongs to the chat service and must stay")
	}
	// The same upload can be indexed again.
	if _, err := e.s.AddKnowledge(ctx, "a", att); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeRanking(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.bot("a", "Alpha")
	for i, content := range []string{
		"The mongoose is a small carnivore. " + strings.Repeat("Unrelated text. ", 100),
		"Mongoose mongoose mongoose habits.",
	} {
		if _, err := e.s.AddKnowledge(ctx, "a", e.attachment(fmt.Sprintf("f%d.txt", i), "text/plain", []byte(content))); err != nil {
			t.Fatal(err)
		}
	}
	res := e.call(chat.ToolKnowledge, newRun(a), map[string]any{"query": "mongoose"})
	first := strings.Index(res.Text, "[1]")
	if first < 0 || !strings.Contains(res.Text[first:first+80], "f1.txt") {
		t.Errorf("best match should come first:\n%s", res.Text)
	}

	// Files still being indexed (or failed) are invisible.
	att := e.attachment("pending.txt", "text/plain", []byte("numbat secrets"))
	e.db.Exec(`INSERT INTO knowledge_files(attachment_id, bot_id, status, created_at) VALUES (?, 'a', 'indexing', 1)`, att)
	e.db.Exec(`INSERT INTO knowledge_chunks(bot_id, attachment_id, ord, text) VALUES ('a', ?, 0, 'numbat secrets')`, att)
	if res := e.call(chat.ToolKnowledge, newRun(a), map[string]any{"query": "numbat"}); strings.Contains(res.Text, "secrets") {
		t.Errorf("unfinished file searchable: %s", res.Text)
	}
}

func TestKnowledgeRejections(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.bot("a", "Alpha")
	e.bot("b", "Beta")
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	cases := []struct {
		why, name, mime string
		data            []byte
		want            string
	}{
		{"pdf by mime", "a.pdf", "application/pdf", []byte("%PDF-1.7 whatever"), "PDF knowledge indexing is not supported yet; attach it to a message instead"},
		{"pdf by bytes", "a.bin", "application/octet-stream", []byte("%PDF-1.4\n..."), "PDF knowledge indexing is not supported yet"},
		{"image", "a.png", "image/png", png, "images cannot be indexed"},
		{"binary", "a.dat", "application/octet-stream", []byte("abc\x00def\x01\x02"), "not a text file"},
		{"blank", "a.txt", "text/plain", []byte(" \n\n "), "no text to index"},
	}
	for _, tc := range cases {
		att := e.attachment(tc.name, tc.mime, tc.data)
		_, err := e.s.AddKnowledge(ctx, "a", att)
		if !errors.Is(err, chat.ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v", tc.why, err)
		}
		if n := e.count(`SELECT COUNT(*) FROM knowledge_files`); n != 0 {
			t.Errorf("%s: left %d knowledge rows", tc.why, n)
		}
	}

	if _, err := e.s.AddKnowledge(ctx, "a", "no-such-attachment"); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unknown attachment = %v", err)
	}
	if _, err := e.s.AddKnowledge(ctx, "ghost", "x"); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unknown bot = %v", err)
	}
	ok := e.attachment("ok.txt", "text/plain", []byte("some knowledge about owls"))
	e.db.Exec(`UPDATE chat_attachments SET bot_id = 'b' WHERE id = ?`, ok)
	if _, err := e.s.AddKnowledge(ctx, "a", ok); !errors.Is(err, chat.ErrInvalid) {
		t.Errorf("another bot's attachment = %v", err)
	}
	e.db.Exec(`UPDATE chat_attachments SET bot_id = NULL WHERE id = ?`, ok)
	if _, err := e.s.AddKnowledge(ctx, "a", ok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.AddKnowledge(ctx, "a", ok); !errors.Is(err, chat.ErrInvalid) || !strings.Contains(err.Error(), "already") {
		t.Errorf("duplicate = %v", err)
	}
	// Same blob for another bot is fine: knowledge is per bot.
	if _, err := e.s.AddKnowledge(ctx, "b", ok); err != nil {
		t.Errorf("sharing a blob between bots: %v", err)
	}

	// Missing blob.
	gone := e.attachment("gone.txt", "text/plain", []byte("vanished"))
	e.db.Exec(`UPDATE chat_attachments SET sha256 = ? WHERE id = ?`, strings.Repeat("0", 64), gone)
	if _, err := e.s.AddKnowledge(ctx, "a", gone); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("missing blob = %v", err)
	}
}

func TestKnowledgeBlobPathsStayInsideTheStore(t *testing.T) {
	e := newEnv(t)
	e.attachment("seed.txt", "text/plain", []byte("creates the store directory"))
	for _, sha := range []string{
		strings.Repeat("..", 32), "../" + strings.Repeat("a", 61), strings.Repeat("A", 64), strings.Repeat("g", 64),
		strings.Repeat("a", 63) + "/", "short", "", `..\..\x` + strings.Repeat("a", 57),
	} {
		if _, err := e.s.readBlob(sha, 1024); err == nil {
			t.Errorf("readBlob(%q) succeeded", sha)
		}
	}
}

func TestKnowledgeFileLimits(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.bot("a", "Alpha")
	var first string
	for i := range maxKnowledgeFiles {
		att := e.attachment(fmt.Sprintf("f%d.txt", i), "text/plain", []byte(fmt.Sprintf("document number %d", i)))
		if i == 0 {
			first = att
		}
		if _, err := e.s.AddKnowledge(ctx, "a", att); err != nil {
			t.Fatalf("file %d: %v", i, err)
		}
	}
	extra := e.attachment("extra.txt", "text/plain", []byte("one too many"))
	_, err := e.s.AddKnowledge(ctx, "a", extra)
	if !errors.Is(err, chat.ErrInvalid) || !strings.Contains(err.Error(), "at most 10") {
		t.Fatalf("11th file = %v", err)
	}
	if err := e.s.RemoveKnowledge(ctx, "a", first); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.AddKnowledge(ctx, "a", extra); err != nil {
		t.Fatal(err)
	}

	// An earlier failed attempt is replaced rather than blocking the file.
	bad := e.attachment("bad.txt", "text/plain", []byte("retry me"))
	e.s.RemoveKnowledge(ctx, "a", extra)
	e.db.Exec(`INSERT INTO knowledge_files(attachment_id, bot_id, status, error, created_at) VALUES (?, 'a', 'error', 'boom', 1)`, bad)
	kf, err := e.s.AddKnowledge(ctx, "a", bad)
	if err != nil || kf.Status != "ready" || kf.Error != "" {
		t.Fatalf("retry after error: %+v, %v", kf, err)
	}
}

func TestKnowledgeLargeFileIsTruncatedWithANote(t *testing.T) {
	e := newEnv(t)
	e.bot("a", "Alpha")
	data := []byte(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 120_000)) // ~5.4 MB
	att := e.attachment("big.txt", "text/plain", data)
	kf, err := e.s.AddKnowledge(context.Background(), "a", att)
	if err != nil {
		t.Fatal(err)
	}
	if kf.Status != "ready" || kf.Chunks == 0 || kf.Chunks > maxChunksPerFile || !strings.Contains(kf.Error, "1 MiB") {
		t.Fatalf("file = %+v", kf)
	}
	if int64(len(data)) <= extract.MaxTextBytes {
		t.Fatal("test data too small")
	}
}
