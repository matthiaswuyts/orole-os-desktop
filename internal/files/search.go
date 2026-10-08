package files

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const searchBudget = 3 * time.Second

var ignoredDirs = map[string]bool{".git": true, "node_modules": true, "target": true, "$recycle.bin": true, "system volume information": true}

// Search implements Provider: case-insensitive name match where every
// space-separated token of query must occur in the name. It never follows
// links, stops at the result and effort limits, and returns what it found when
// its time budget runs out.
func (l *Local) Search(ctx context.Context, path, query string, opt SearchOptions) ([]Entry, error) {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return []Entry{}, nil
	}
	r, err := l.resolve(ctx, path, true)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(r.real)
	if err != nil {
		return nil, mapErr(err, r.display)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDir, r.display)
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = DefaultSearchResults
	}
	if limit > MaxSearchResults {
		limit = MaxSearchResults
	}
	ctx, cancel := context.WithTimeout(ctx, searchBudget)
	defer cancel()

	type hit struct {
		e     Entry
		score int
	}
	var hits []hit
	visited := 0
	collectCap := limit * 4
	_ = filepath.WalkDir(r.real, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == r.real {
			return nil
		}
		visited++
		if visited > MaxSearchVisited || ctx.Err() != nil || len(hits) >= collectCap {
			return fs.SkipAll
		}
		name := d.Name()
		hidden := strings.HasPrefix(name, ".")
		if d.IsDir() && !opt.IncludeHidden && (ignoredDirs[strings.ToLower(name)] || hidden) {
			return fs.SkipDir
		}
		if hidden && !opt.IncludeHidden {
			return nil
		}
		lname := strings.ToLower(name)
		score := 0
		for _, t := range tokens {
			i := strings.Index(lname, t)
			if i < 0 {
				return nil
			}
			switch {
			case lname == t:
				score += 100
			case i == 0:
				score += 50
			default:
				score += 20 - min(i, 19)
			}
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(r.real, p)
		if err != nil {
			return nil
		}
		link := isLinkMode(info.Mode())
		var td *bool
		if link {
			b := false
			if t, err := os.Stat(p); err == nil {
				b = t.IsDir()
			}
			td = &b
		}
		e := entryOf(filepath.Join(r.display, rel), info, link, td)
		hits = append(hits, hit{e, score - strings.Count(rel, string(filepath.Separator))})
		return nil
	})
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return len(hits[i].e.Path) < len(hits[j].e.Path)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out, nil
}
