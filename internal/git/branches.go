package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Branch is a local or remote-tracking branch.
type Branch struct {
	Name     string `json:"name"`
	Remote   bool   `json:"remote"`
	Current  bool   `json:"current"`
	Hash     string `json:"hash"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Gone     bool   `json:"gone"`
	Date     int64  `json:"date"`
	Subject  string `json:"subject"`
}

// Branches lists local and remote-tracking branches, newest commit first.
func Branches(ctx context.Context, dir string) ([]Branch, error) {
	const f = "%(refname)%00%(refname:short)%00%(objectname:short)%00%(HEAD)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(committerdate:unix)%00%(contents:subject)"
	out, err := runDefault(ctx, dir, "for-each-ref", "--format="+f, "--sort=-committerdate", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	res := []Branch{}
	for _, ln := range strings.Split(string(out), "\n") {
		p := strings.Split(ln, "\x00")
		if len(p) < 8 {
			continue
		}
		if strings.HasSuffix(p[0], "/HEAD") && strings.HasPrefix(p[0], "refs/remotes/") {
			continue
		}
		b := Branch{Name: p[1], Remote: strings.HasPrefix(p[0], "refs/remotes/"), Hash: p[2], Current: p[3] == "*", Upstream: p[4], Subject: p[7]}
		b.Date, _ = strconv.ParseInt(p[6], 10, 64)
		for _, part := range strings.Split(p[5], ",") {
			part = strings.TrimSpace(part)
			switch {
			case part == "gone":
				b.Gone = true
			case strings.HasPrefix(part, "ahead "):
				b.Ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "ahead "))
			case strings.HasPrefix(part, "behind "):
				b.Behind, _ = strconv.Atoi(strings.TrimPrefix(part, "behind "))
			}
		}
		res = append(res, b)
	}
	return res, nil
}

// CreateBranch creates branch name at start (empty = HEAD) without checking it out.
func CreateBranch(ctx context.Context, dir, name, start string) error {
	if err := ValidBranchName(ctx, name); err != nil {
		return err
	}
	args := []string{"branch", "--no-track", "--", name}
	if start != "" {
		if !ValidRev(start) {
			return fmt.Errorf("%w: %q", ErrInvalidRef, start)
		}
		args = append(args, start)
	}
	_, err := runDefault(ctx, dir, args...)
	return err
}

// BranchExists reports whether refs/heads/<name> exists.
func BranchExists(ctx context.Context, dir, name string) (bool, error) {
	if !ValidRef(name) {
		return false, fmt.Errorf("%w: %q", ErrInvalidRef, name)
	}
	_, err := runDefault(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	if ge, ok := err.(*Error); ok && strings.TrimSpace(ge.Stderr) == "" {
		return false, nil
	}
	return false, err
}

// Commit is one log entry.
type Commit struct {
	Hash    string   `json:"hash"`
	Short   string   `json:"short"`
	Author  string   `json:"author"`
	Email   string   `json:"email"`
	Date    int64    `json:"date"`
	Refs    []string `json:"refs"`
	Subject string   `json:"subject"`
}

// Log returns up to limit commits reachable from ref (empty = HEAD). A repository with no
// commits yields an empty list.
func Log(ctx context.Context, dir, ref string, limit int) ([]Commit, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	args := []string{"log", "-n", strconv.Itoa(limit), "--format=%H%x1f%h%x1f%an%x1f%ae%x1f%at%x1f%D%x1f%s%x1e"}
	if ref != "" {
		if !ValidRev(ref) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidRef, ref)
		}
		args = append(args, ref)
	}
	args = append(args, "--")
	out, err := runDefault(ctx, dir, args...)
	if err != nil {
		if ge, ok := err.(*Error); ok && strings.Contains(ge.Stderr, "does not have any commits yet") {
			return []Commit{}, nil
		}
		return nil, err
	}
	res := []Commit{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		p := strings.Split(rec, "\x1f")
		if len(p) < 7 {
			continue
		}
		c := Commit{Hash: p[0], Short: p[1], Author: p[2], Email: p[3], Subject: p[6], Refs: []string{}}
		c.Date, _ = strconv.ParseInt(p[4], 10, 64)
		if p[5] != "" {
			c.Refs = append(c.Refs, strings.Split(p[5], ", ")...)
		}
		res = append(res, c)
	}
	return res, nil
}
