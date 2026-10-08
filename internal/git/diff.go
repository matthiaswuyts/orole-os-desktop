package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// DiffLine is one line of a hunk. Kind is " " (context), "+" or "-".
// Old/New are 1-based line numbers, 0 when not applicable.
type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	Old  int    `json:"old"`
	New  int    `json:"new"`
}

// DiffHunk is one @@ section.
type DiffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffFile is one changed file. Status: added | deleted | modified | renamed | copied.
type DiffFile struct {
	Path      string     `json:"path"`
	OldPath   string     `json:"oldPath,omitempty"`
	Status    string     `json:"status"`
	Binary    bool       `json:"binary"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Hunks     []DiffHunk `json:"hunks"`
}

// DiffResult is a parsed diff. Truncated is set when output exceeded the size cap.
type DiffResult struct {
	Files     []DiffFile `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Truncated bool       `json:"truncated"`
}

// DiffOptions selects what to diff:
//
//	{}                      unstaged changes (work tree vs index)
//	{Staged: true}          staged changes (index vs HEAD)
//	{From: "HEAD"}          work tree vs a ref
//	{From: a, To: b}        between two refs
//
// Untracked files are not included (git does not diff them).
type DiffOptions struct {
	Staged  bool   `json:"staged"`
	From    string `json:"from"`
	To      string `json:"to"`
	Path    string `json:"path"` // optional pathspec limiting the diff
	Context int    `json:"context"`
}

// Diff runs git diff in dir and parses the result.
func Diff(ctx context.Context, dir string, o DiffOptions) (DiffResult, error) {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--ignore-submodules=dirty", "-M", "--src-prefix=a/", "--dst-prefix=b/"}
	if o.Context > 0 && o.Context <= 1000 {
		args = append(args, "-U"+strconv.Itoa(o.Context))
	}
	if o.To != "" && o.From == "" {
		return DiffResult{}, fmt.Errorf("git diff: 'to' requires 'from'")
	}
	if o.Staged && (o.From != "" || o.To != "") {
		return DiffResult{}, fmt.Errorf("git diff: staged cannot be combined with refs")
	}
	if o.Staged {
		args = append(args, "--cached")
	}
	for _, r := range []string{o.From, o.To} {
		if r != "" && !ValidRev(r) {
			return DiffResult{}, fmt.Errorf("%w: %q", ErrInvalidRef, r)
		}
	}
	if o.From != "" {
		args = append(args, o.From)
	}
	if o.To != "" {
		args = append(args, o.To)
	}
	args = append(args, "--")
	if o.Path != "" {
		if strings.ContainsRune(o.Path, 0) {
			return DiffResult{}, fmt.Errorf("git diff: invalid path")
		}
		args = append(args, ":(literal)"+o.Path)
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	repo, err := openReviewRepo(ctx, dir)
	if err != nil {
		return DiffResult{}, err
	}
	defer repo.close()
	out, trunc, err := repo.run(ctx, dir, nil, args...)
	if err != nil {
		return DiffResult{}, err
	}
	res := ParseDiff(string(out))
	res.Truncated = trunc
	if !trunc {
		if err := repo.addDirtySubmoduleDiff(ctx, dir, o, &res); err != nil {
			return DiffResult{}, err
		}
	}
	return res, nil
}

// ParseDiff parses unified diff text produced by `git diff`.
func ParseDiff(text string) DiffResult {
	res := DiffResult{Files: []DiffFile{}}
	var cur *DiffFile
	var hunk *DiffHunk
	var oldN, newN int
	flush := func() {
		if hunk != nil && cur != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
		}
		hunk = nil
	}
	finish := func() {
		flush()
		if cur != nil {
			res.Files = append(res.Files, *cur)
			res.Additions += cur.Additions
			res.Deletions += cur.Deletions
		}
		cur = nil
	}
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for _, ln := range lines {
		ln = strings.TrimSuffix(ln, "\r")
		if strings.HasPrefix(ln, "diff --git ") {
			finish()
			p := guessPath(strings.TrimPrefix(ln, "diff --git "))
			cur = &DiffFile{Path: p, Status: "modified", Hunks: []DiffHunk{}}
			continue
		}
		if cur == nil {
			continue
		}
		if hunk == nil {
			switch {
			case strings.HasPrefix(ln, "new file mode"):
				cur.Status = "added"
			case strings.HasPrefix(ln, "deleted file mode"):
				cur.Status = "deleted"
			case strings.HasPrefix(ln, "rename from "):
				cur.Status, cur.OldPath = "renamed", strings.TrimPrefix(ln, "rename from ")
			case strings.HasPrefix(ln, "rename to "):
				cur.Path = strings.TrimPrefix(ln, "rename to ")
			case strings.HasPrefix(ln, "copy from "):
				cur.Status, cur.OldPath = "copied", strings.TrimPrefix(ln, "copy from ")
			case strings.HasPrefix(ln, "copy to "):
				cur.Path = strings.TrimPrefix(ln, "copy to ")
			case strings.HasPrefix(ln, "Binary files "), ln == "GIT binary patch":
				cur.Binary = true
			case strings.HasPrefix(ln, "+++ b/"):
				cur.Path = strings.TrimPrefix(ln, "+++ b/")
			case strings.HasPrefix(ln, "--- a/") && cur.OldPath == "" && cur.Status != "added":
				if p := strings.TrimPrefix(ln, "--- a/"); p != cur.Path {
					cur.OldPath = p
				}
			}
		}
		if strings.HasPrefix(ln, "@@") {
			flush()
			h, ok := parseHunkHeader(ln)
			if !ok {
				continue
			}
			hunk = &h
			oldN, newN = h.OldStart, h.NewStart
			continue
		}
		if hunk == nil {
			continue
		}
		if ln == "" { // blank context line whose leading space was stripped
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: " ", Old: oldN, New: newN})
			oldN++
			newN++
			continue
		}
		switch ln[0] {
		case '+':
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: "+", Text: ln[1:], New: newN})
			newN++
			cur.Additions++
		case '-':
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: "-", Text: ln[1:], Old: oldN})
			oldN++
			cur.Deletions++
		case ' ':
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: " ", Text: ln[1:], Old: oldN, New: newN})
			oldN++
			newN++
		case '\\': // "\ No newline at end of file"
		}
	}
	finish()
	return res
}

// guessPath extracts the b-side path from "a/x b/x" (exact when both sides match).
func guessPath(s string) string {
	if half := (len(s) - 1) / 2; len(s)%2 == 1 && s[half] == ' ' && strings.HasPrefix(s, "a/") && strings.TrimPrefix(s[:half], "a/") == strings.TrimPrefix(s[half+1:], "b/") {
		return strings.TrimPrefix(s[half+1:], "b/")
	}
	if i := strings.LastIndex(s, " b/"); i >= 0 {
		return s[i+3:]
	}
	return s
}

// parseHunkHeader parses "@@ -a,b +c,d @@ section".
func parseHunkHeader(ln string) (DiffHunk, bool) {
	h := DiffHunk{Header: ln}
	rest := strings.TrimPrefix(ln, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return h, false
	}
	parts := strings.Fields(rest[:end])
	if len(parts) != 2 || parts[0][0] != '-' || parts[1][0] != '+' {
		return h, false
	}
	var ok1, ok2 bool
	h.OldStart, h.OldLines, ok1 = parseRange(parts[0][1:])
	h.NewStart, h.NewLines, ok2 = parseRange(parts[1][1:])
	return h, ok1 && ok2
}

func parseRange(s string) (start, n int, ok bool) {
	a, b, hasN := strings.Cut(s, ",")
	start, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, false
	}
	n = 1
	if hasN {
		if n, err = strconv.Atoi(b); err != nil {
			return 0, 0, false
		}
	}
	return start, n, true
}
