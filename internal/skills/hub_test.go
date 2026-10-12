package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillMD(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\nDo the thing.\n"
}

// archive builds a repository zip the way GitHub does: one top-level directory.
func archive(t *testing.T, files map[string]string, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create("repo-abc123/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	for name, target := range links {
		h := &zip.FileHeader{Name: "repo-abc123/" + name}
		h.SetMode(os.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(target))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hubFor(t *testing.T, mux http.Handler) *Hub {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	h := NewHub()
	h.SiteURL, h.GitHubURL = srv.URL, srv.URL
	return h
}

func serveArchive(zipBytes []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acme/skills/archive/HEAD.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipBytes)
	})
}

func TestHubInstallCopiesOnlyTheSkillDirectory(t *testing.T) {
	zipBytes := archive(t, map[string]string{
		"skills/seo-audit/SKILL.md":           skillMD("seo-audit", "Audit SEO"),
		"skills/seo-audit/references/tips.md": "tips",
		"skills/seo-audit/scripts/run.sh":     "echo hi",
		"skills/seo-audit/.hidden/secret":     "x",
		"skills/seo-audit/../evil.txt":        "x",
		"skills/other/SKILL.md":               skillMD("other", "Other"),
		"skills/other/notes.md":               "not ours",
		"README.md":                           "readme",
	}, map[string]string{"skills/seo-audit/link.md": "/etc/passwd"})
	h := hubFor(t, serveArchive(zipBytes))
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	p, err := h.Preview(context.Background(), "acme/skills", "seo-audit")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "seo-audit" || p.Description != "Audit SEO" || strings.Join(p.Files, ",") != "references/tips.md,scripts/run.sh" || strings.Join(p.Scripts, ",") != "scripts/run.sh" {
		t.Fatalf("preview = %+v", p)
	}
	if list, _ := st.List(); len(list) != 0 {
		t.Fatalf("preview wrote to the registry: %v", list)
	}

	sk, err := h.Install(context.Background(), st, "acme/skills", "seo-audit")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Source != "skills.sh:acme/skills/seo-audit" || !sk.Enabled {
		t.Fatalf("skill = %+v", sk)
	}
	for _, rel := range []string{"SKILL.md", "references/tips.md", "scripts/run.sh"} {
		if _, err := os.Stat(filepath.Join(st.Root(), "seo-audit", filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s not copied: %v", rel, err)
		}
	}
	for _, rel := range []string{"link.md", ".hidden/secret", "notes.md"} {
		if _, err := os.Lstat(filepath.Join(st.Root(), "seo-audit", filepath.FromSlash(rel))); err == nil {
			t.Fatalf("%s must not be copied", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(st.Root(), "evil.txt")); err == nil {
		t.Fatal("path traversal escaped the skill directory")
	}
	// Installing again must not overwrite what the user may have edited.
	if _, err := h.Install(context.Background(), st, "acme/skills", "seo-audit"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second install = %v, want ErrInvalid", err)
	}
}

func TestHubFindsSkillByFrontmatterNameWhenDirectoryDiffers(t *testing.T) {
	zipBytes := archive(t, map[string]string{
		"plugins/a/skills/dir-name/SKILL.md": skillMD("real-name", "Found by name"),
		"plugins/b/SKILL.md":                 skillMD("unrelated", "No"),
	}, nil)
	h := hubFor(t, serveArchive(zipBytes))
	p, err := h.Preview(context.Background(), "acme/skills", "real-name")
	if err != nil || p.Name != "real-name" {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	if _, err := h.Preview(context.Background(), "acme/skills", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing skill = %v, want ErrNotFound", err)
	}
}

func TestHubRejectsUnsafeReferencesBeforeAnyRequest(t *testing.T) {
	hits := 0
	h := hubFor(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	for _, c := range [][2]string{{"acme", "x"}, {"acme/skills/../x", "x"}, {"../skills", "x"}, {"acme/skills", "../x"}, {"acme/skills", "a/b"}, {"acme/skills", ""}} {
		if _, err := h.Preview(context.Background(), c[0], c[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("Preview(%q,%q) = %v, want ErrInvalid", c[0], c[1], err)
		}
	}
	if hits != 0 {
		t.Fatalf("%d requests were made", hits)
	}
}

func TestHubPrivateRepositoryIsNotFound(t *testing.T) {
	h := hubFor(t, http.NotFoundHandler())
	if _, err := h.Preview(context.Background(), "acme/private", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestHubBrowseAndSearchDropEntriesThatCannotBeInstalled(t *testing.T) {
	h := hubFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/skills/all-time/2":
			_, _ = w.Write([]byte(`{"skills":[{"source":"vercel-labs/skills","skillId":"find-skills","name":"find-skills","installs":12,"isOfficial":true},{"source":"api/git","skillId":"..","name":"bad"},{"source":"not-a-repo","skillId":"x","name":"bad"}],"hasMore":true,"total":99,"page":2}`))
		case r.URL.Path == "/api/search" && r.URL.Query().Get("q") == "seo audit":
			_, _ = w.Write([]byte(`{"skills":[{"source":"a/b","skillId":"seo","name":"seo\u202e","installs":-5}]}`))
		default:
			http.Error(w, "no", http.StatusBadRequest)
		}
	}))
	page, err := h.Browse(context.Background(), "all-time", 2)
	if err != nil || len(page.Skills) != 1 || !page.Skills[0].Official || !page.HasMore || page.Page != 2 || page.Total != 99 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	found, err := h.Search(context.Background(), "  seo   audit ")
	if err != nil || len(found.Skills) != 1 || found.Skills[0].Installs != 0 || strings.ContainsRune(found.Skills[0].Name, '\u202e') {
		t.Fatalf("search = %+v, %v", found, err)
	}
	for _, bad := range []func() error{
		func() error { _, err := h.Browse(context.Background(), "../x", 0); return err },
		func() error { _, err := h.Browse(context.Background(), "trending", -1); return err },
		func() error { _, err := h.Search(context.Background(), "a"); return err },
	} {
		if err := bad(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
	}
	if _, err := h.Browse(context.Background(), "trending", 0); !errors.Is(err, ErrHub) {
		t.Fatalf("upstream error = %v, want ErrHub", err)
	}
}

func TestHubDescribeReadsMetaDescriptionAndCaches(t *testing.T) {
	hits := 0
	h := hubFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acme/skills/seo" {
			http.NotFound(w, r)
			return
		}
		hits++
		_, _ = w.Write([]byte(`<html><head><meta name="description" content="Audit &quot;SEO&quot; &amp; more"/></head></html>`))
	}))
	refs := []HubRef{{"acme/skills", "seo"}, {"acme/skills", "gone"}, {"bad", "x"}}
	for range 2 {
		got := h.Describe(context.Background(), refs)
		if got[0].Description != `Audit "SEO" & more` || got[1].Description != "" || got[2].Description != "" {
			t.Fatalf("descriptions = %+v", got)
		}
	}
	if hits != 1 {
		t.Fatalf("page fetched %d times, want 1 (cached)", hits)
	}
}

func TestHubTopicReadsSkillLinksFromThePage(t *testing.T) {
	h := hubFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/topic/marketing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<a href="/_next/static/immutable/x.css"></a><a href="/topic/design"></a><a href="/docs/api/x">d</a>` +
			`<a href=\"/acme/skills/seo-audit\">a</a><a href="/acme/skills/copywriting">b</a><a href="/acme/skills/seo-audit">dup</a><a href="/a/b">short</a>`))
	}))
	page, err := h.Topic(context.Background(), "marketing")
	if err != nil || len(page.Skills) != 2 || page.Skills[0].SkillID != "seo-audit" || page.Skills[1].SkillID != "copywriting" || page.Skills[0].Source != "acme/skills" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if _, err := h.Topic(context.Background(), "../x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown topic = %v, want ErrInvalid", err)
	}
}
