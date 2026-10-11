package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The disposable repository's config is written as a file rather than through
// `git config --add`; git itself must read back exactly the allow-listed entries.
func TestWriteReviewConfigRoundTrip(t *testing.T) {
	entries := []struct{ key, value string }{
		{"core.autocrlf", "input"},
		{"core.excludesfile", `C:\Users\me\.gitignore "quoted"`},
		{"branch.feature/x.remote", "origin"},
		{"branch.feature/x.merge", "refs/heads/feature/x"},
		{"branch.we.ird.sub.remote", "up\tstream"},
		{"remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"remote.origin.fetch", "+refs/tags/*:refs/tags/*"},
		{"remote.o\"r\\g.fetch", "+a:b"},
		{"diff.img.binary", "true"},
		{"core.ignorecase", ""},
		// Filtered: executable or remote-controlling keys must never be written.
		{"core.fsmonitor", "evil"}, {"filter.x.clean", "evil"}, {"remote.origin.url", "https://example.invalid"},
		{"alias.x", "!evil"}, {"include.path", "evil"},
	}
	var list strings.Builder
	for _, e := range entries {
		list.WriteString(e.key + "\n" + e.value + "\x00")
	}
	// A multi-line value must stay inside its quotes rather than start a new section.
	list.WriteString("core.eol\nlf\n[core]\n\tfsmonitor = evil\x00")
	// An implicit-true key has no "\n" separator in --null output and is skipped.
	list.WriteString("core.symlinks\x00")
	var b strings.Builder
	writeReviewConfig(&b, []byte(list.String()))
	file := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(file, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "config", "--file", file, "--null", "--list").CombinedOutput()
	if err != nil {
		t.Fatalf("git cannot read generated config: %v\n%s\n%s", err, out, b.String())
	}
	var want strings.Builder
	for _, e := range entries {
		if reviewConfigKey(e.key) {
			want.WriteString(strings.ToLower(e.key[:strings.IndexByte(e.key, '.')]) + e.key[strings.IndexByte(e.key, '.'):] + "\n" + e.value + "\x00")
		}
	}
	want.WriteString("core.eol\nlf\n[core]\n\tfsmonitor = evil\x00")
	if string(out) != want.String() {
		t.Fatalf("round trip mismatch\n got %q\nwant %q\nfile:\n%s", out, want.String(), b.String())
	}
}
