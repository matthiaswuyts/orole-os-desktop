package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// countDB counts the statements the manager sends to SQLite.
type countDB struct {
	DB
	sessionWrites, eventStatements atomic.Int64
}

func (c *countDB) Exec(query string, args ...any) (sql.Result, error) {
	switch {
	case strings.HasPrefix(query, "INSERT INTO agent_sessions"):
		c.sessionWrites.Add(1)
	case strings.HasPrefix(query, "INSERT INTO agent_events"):
		c.eventStatements.Add(1)
	}
	return c.DB.Exec(query, args...)
}

// BenchmarkHotEventFlow drives the per-event path of a live structured session (assistant
// message, tool call, tool result, activity) while a reader polls Manager.Get, the way the UI
// does. It reports throughput, bytes handed to Publish per event, SQLite statements per event
// and how long a reader waits for the manager lock.
func BenchmarkHotEventFlow(b *testing.B) {
	db, err := database.Open(filepath.Join(b.TempDir(), "bench.db"), migrations.FS)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	cdb := &countDB{DB: db}
	var published atomic.Int64
	m := NewManager(Config{
		Store: NewStore(cdb),
		Publish: func(_ string, data any) {
			raw, _ := json.Marshal(data)
			published.Add(int64(len(raw)))
		},
		Authorize: func(context.Context, string, string, string) error { return nil },
	})
	now := time.Now().UTC()
	s := Session{ID: "bench", Kind: "claude", Machine: "local", Mode: ModeStructured, Status: StatusWorking, Task: strings.Repeat("task ", 40),
		LastMessage: strings.Repeat("previous answer ", 100), StartedAt: now, UpdatedAt: now}
	ls := &liveSess{id: s.ID, s: s, proc: &chatTestProcess{}, done: make(chan struct{})}
	m.mu.Lock()
	m.live[s.ID] = ls
	m.mu.Unlock()
	if err := m.st.SaveSession(s, ""); err != nil {
		b.Fatal(err)
	}

	var (
		stop    = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		waits   []time.Duration
		message = strings.Repeat("The agent is working on the next step of the plan. ", 4)
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			start := time.Now()
			m.mu.Lock()
			d := time.Since(start)
			m.mu.Unlock()
			mu.Lock()
			waits = append(waits, d)
			mu.Unlock()
			time.Sleep(50 * time.Microsecond)
		}
	}()

	begin := time.Now()
	events := 0
	for b.Loop() {
		for _, e := range []Event{
			{Kind: EvMessage, Text: message},
			{Kind: EvTool, Text: "Read internal/agents/manager.go", Data: jraw(map[string]string{"id": "t"})},
			{Kind: EvToolResult, Text: "package agents\n// ...", Data: jraw(map[string]string{"id": "t"})},
			{Kind: EvActivity, Text: "Thinking"},
		} {
			m.apply("bench", e)
			events++
		}
	}
	elapsed := time.Since(begin)
	b.StopTimer()
	close(stop)
	wg.Wait()
	drainWrites(m)

	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	pick := func(q float64) float64 {
		if len(waits) == 0 {
			return 0
		}
		return float64(waits[int(float64(len(waits)-1)*q)].Microseconds())
	}
	b.ReportMetric(float64(events)/elapsed.Seconds(), "events/s")
	b.ReportMetric(float64(published.Load())/float64(events), "published-B/event")
	b.ReportMetric(float64(cdb.sessionWrites.Load())/float64(events), "session-writes/event")
	b.ReportMetric(float64(cdb.eventStatements.Load())/float64(events), "event-stmts/event")
	b.ReportMetric(pick(0.5), "lockwait-p50-µs")
	b.ReportMetric(pick(0.99), "lockwait-p99-µs")
	b.ReportMetric(pick(1), "lockwait-max-µs")
}

// drainWrites waits until every deferred write has reached SQLite.
func drainWrites(m *Manager) {
	_ = m.w.close()
	_ = m.st.FlushEvents()
}
