package sandbox

import (
	"strconv"
	"sync"
	"time"
)

// Kinds of ActivityEntry.
const (
	ActivityCommand = "command" // computer_run
	ActivityFile    = "file"    // computer_write_file
	ActivityBrowser = "browser" // browser_open, click, type, read, screenshot
)

// States of an ActivityEntry.
const (
	ActivityRunning = "running"
	ActivityDone    = "done"
	ActivityFailed  = "failed"
)

// activityMax is how many entries are kept per bot; the oldest are dropped first.
const activityMax = 100

// ActivityEntry is one thing a bot did on its computer, for the Computer pane's activity tab. Title and Output are
// untrusted text the caller has already cleaned and capped.
type ActivityEntry struct {
	ID         string `json:"id"`
	BotID      string `json:"botId"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	StartedAt  string `json:"startedAt"` // RFC 3339
	DurationMs int64  `json:"durationMs"`
}

// Activity keeps the last activityMax entries per bot, in memory only: they are history for the open pane, not a
// record, and go away with the daemon. Every change is passed to publish. A nil *Activity ignores everything.
type Activity struct {
	mu      sync.Mutex
	seq     uint64
	byBot   map[string][]*ActivityEntry
	started map[string]time.Time
	publish func(ActivityEntry)
	now     func() time.Time
}

// NewActivity returns an Activity that reports every new or finished entry to publish (which may be nil).
func NewActivity(publish func(ActivityEntry)) *Activity {
	return &Activity{byBot: map[string][]*ActivityEntry{}, started: map[string]time.Time{}, publish: publish, now: time.Now}
}

// Begin records a running entry and returns its id for End.
func (a *Activity) Begin(botID, kind, title string) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	a.seq++
	now := a.now()
	e := &ActivityEntry{ID: strconv.FormatUint(a.seq, 10), BotID: botID, Kind: kind, Title: title, Status: ActivityRunning, StartedAt: now.UTC().Format(time.RFC3339)}
	list := append(a.byBot[botID], e)
	if len(list) > activityMax {
		for _, old := range list[:len(list)-activityMax] {
			delete(a.started, old.ID)
		}
		list = list[len(list)-activityMax:]
	}
	a.byBot[botID] = list
	a.started[e.ID] = now
	snap := *e
	a.mu.Unlock()
	a.emit(snap)
	return e.ID
}

// End finishes the entry Begin returned.
func (a *Activity) End(botID, id, status, output string, exitCode int, truncated bool) {
	if a == nil || id == "" {
		return
	}
	a.mu.Lock()
	var snap ActivityEntry
	found := false
	for _, e := range a.byBot[botID] {
		if e.ID != id {
			continue
		}
		e.Status, e.Output, e.ExitCode, e.Truncated = status, output, exitCode, truncated
		e.DurationMs = a.now().Sub(a.started[id]).Milliseconds()
		delete(a.started, id)
		snap, found = *e, true
		break
	}
	a.mu.Unlock()
	if found {
		a.emit(snap)
	}
}

// List returns the bot's entries, oldest first.
func (a *Activity) List(botID string) []ActivityEntry {
	out := []ActivityEntry{}
	if a == nil {
		return out
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.byBot[botID] {
		out = append(out, *e)
	}
	return out
}

func (a *Activity) emit(e ActivityEntry) {
	if a.publish != nil {
		a.publish(e)
	}
}
