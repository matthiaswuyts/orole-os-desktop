package git

import (
	"context"
	"fmt"
	"strings"
)

// BranchState is the head state reported by status.
type BranchState struct {
	OID      string `json:"oid"`
	Head     string `json:"head"` // branch name, or "(detached)"
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
}

// FileStatus is one changed path. Index and Worktree are the porcelain X/Y letters
// ("." = unchanged); Kind is a coarse category for the UI.
type FileStatus struct {
	Path     string `json:"path"`
	OrigPath string `json:"origPath,omitempty"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	Kind     string `json:"kind"` // modified | added | deleted | renamed | untracked | conflicted | ignored
}

// Status is a work tree snapshot.
type Status struct {
	Branch     BranchState  `json:"branch"`
	Files      []FileStatus `json:"files"`
	Staged     int          `json:"staged"`
	Unstaged   int          `json:"unstaged"`
	Untracked  int          `json:"untracked"`
	Conflicted int          `json:"conflicted"`
	// Dirty is the number of paths with any change (staged, unstaged, untracked or conflicted).
	Dirty int `json:"dirty"`
}

// GetStatus runs `git status --porcelain=v2 --branch -z` in dir.
func GetStatus(ctx context.Context, dir string) (Status, error) {
	out, err := runDefault(ctx, dir, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=normal", "--no-renames")
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(out)
}

// ParseStatus parses `git status --porcelain=v2 --branch -z` output.
func ParseStatus(out []byte) (Status, error) {
	st := Status{Files: []FileStatus{}}
	recs := strings.Split(string(out), "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if r == "" {
			continue
		}
		switch {
		case strings.HasPrefix(r, "# branch.oid "):
			st.Branch.OID = strings.TrimPrefix(r, "# branch.oid ")
		case strings.HasPrefix(r, "# branch.head "):
			st.Branch.Head = strings.TrimPrefix(r, "# branch.head ")
		case strings.HasPrefix(r, "# branch.upstream "):
			st.Branch.Upstream = strings.TrimPrefix(r, "# branch.upstream ")
		case strings.HasPrefix(r, "# branch.ab "):
			if _, err := fmt.Sscanf(strings.TrimPrefix(r, "# branch.ab "), "+%d -%d", &st.Branch.Ahead, &st.Branch.Behind); err != nil {
				return st, fmt.Errorf("git status: bad branch.ab %q", r)
			}
		case strings.HasPrefix(r, "# "):
		case r[0] == '1':
			f := strings.SplitN(r, " ", 9)
			if len(f) < 9 {
				return st, fmt.Errorf("git status: malformed record %q", r)
			}
			st.add(f[8], "", f[1])
		case r[0] == '2':
			f := strings.SplitN(r, " ", 10)
			if len(f) < 10 {
				return st, fmt.Errorf("git status: malformed record %q", r)
			}
			orig := ""
			if i+1 < len(recs) {
				i++
				orig = recs[i]
			}
			st.add(f[9], orig, f[1])
		case r[0] == 'u':
			f := strings.SplitN(r, " ", 11)
			if len(f) < 11 {
				return st, fmt.Errorf("git status: malformed record %q", r)
			}
			st.add(f[10], "", f[1])
		case r[0] == '?':
			st.add(strings.TrimPrefix(r, "? "), "", "??")
		case r[0] == '!':
		default:
			return st, fmt.Errorf("git status: unknown record %q", r)
		}
	}
	return st, nil
}

func (s *Status) add(path, orig, xy string) {
	f := FileStatus{Path: path, OrigPath: orig, Index: ".", Worktree: "."}
	switch {
	case xy == "??":
		f.Index, f.Worktree, f.Kind = "?", "?", "untracked"
		s.Untracked++
	default:
		f.Index, f.Worktree = xy[:1], xy[1:2]
		if isConflict(xy) {
			f.Kind = "conflicted"
			s.Conflicted++
		} else {
			if f.Index != "." {
				s.Staged++
			}
			if f.Worktree != "." {
				s.Unstaged++
			}
			f.Kind = kindOf(f.Index, f.Worktree)
		}
	}
	s.Dirty++
	s.Files = append(s.Files, f)
}

func isConflict(xy string) bool {
	switch xy {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

func kindOf(x, y string) string {
	c := x
	if c == "." {
		c = y
	}
	switch c {
	case "A":
		return "added"
	case "D":
		return "deleted"
	case "R", "C":
		return "renamed"
	}
	return "modified"
}
