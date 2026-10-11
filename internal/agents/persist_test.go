package agents

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type recordedPub struct {
	event string
	data  any
}

type pubRecorder struct {
	mu   sync.Mutex
	list []recordedPub
}

func (p *pubRecorder) publish(event string, data any) {
	p.mu.Lock()
	p.list = append(p.list, recordedPub{event, data})
	p.mu.Unlock()
}

func (p *pubRecorder) all(event string) []any {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []any
	for _, e := range p.list {
		if e.event == event {
			out = append(out, e.data)
		}
	}
	return out
}

func (p *pubRecorder) reset() {
	p.mu.Lock()
	p.list = nil
	p.mu.Unlock()
}

// writeCounter counts the statements that reach SQLite.
type writeCounter struct {
	DB
	sessions, events atomic.Int64
}

func (c *writeCounter) Exec(query string, args ...any) (sql.Result, error) {
	switch {
	case strings.HasPrefix(query, "INSERT INTO agent_sessions"):
		c.sessions.Add(1)
	case strings.HasPrefix(query, "INSERT INTO agent_events"):
		c.events.Add(1)
	}
	return c.DB.Exec(query, args...)
}

func persistDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "persist.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// persistFixture is a manager with one live structured session "s".
func persistFixture(t *testing.T) (*Manager, *pubRecorder, *writeCounter) {
	t.Helper()
	db := persistDB(t)
	counter := &writeCounter{DB: db}
	pubs := &pubRecorder{}
	m := NewManager(Config{Store: NewStore(counter), Publish: pubs.publish})
	now := time.Now().UTC()
	s := Session{ID: "s", Kind: "claude", Machine: "local", Mode: ModeStructured, Status: StatusWorking, StartedAt: now, UpdatedAt: now}
	ls := &liveSess{id: s.ID, s: s, proc: &chatTestProcess{}, done: make(chan struct{})}
	m.live[s.ID] = ls
	if err := m.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	counter.sessions.Store(0)
	t.Cleanup(func() {
		m.mu.Lock()
		m.closing = true
		m.mu.Unlock()
		m.bgCancel()
		m.bgWork.Wait()
		_ = m.w.close()
		_ = m.st.FlushEvents()
	})
	return m, pubs, counter
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHotEventsAreDebouncedAndPublishedAsDeltas(t *testing.T) {
	m, pubs, counter := persistFixture(t)
	const n = 200
	for i := range n {
		m.apply("s", Event{Kind: EvMessage, Text: fmt.Sprintf("message %d", i)})
	}
	if got := counter.sessions.Load(); got > 3 {
		t.Fatalf("%d hot events caused %d session writes; want them coalesced", n, got)
	}
	if updates := pubs.all("agents.updated"); len(updates) != 0 {
		t.Fatalf("hot events published %d full sessions", len(updates))
	}
	deltas := pubs.all("agents.delta")
	if len(deltas) != n {
		t.Fatalf("published %d deltas for %d changing events", len(deltas), n)
	}
	last := deltas[n-1].(SessionDelta)
	if last.ID != "s" || last.LastMessage == nil || *last.LastMessage != "message 199" {
		t.Fatalf("last delta = %+v", last)
	}
	raw, _ := json.Marshal(last)
	if strings.Contains(string(raw), `"status"`) || strings.Contains(string(raw), `"task"`) || len(raw) > 400 {
		t.Fatalf("delta is not a small partial payload: %s", raw)
	}
	live, _ := m.Get("s")
	if live.LastMessage != "message 199" {
		t.Fatalf("live view = %q", live.LastMessage)
	}
	// The debounce timer writes the newest state without anyone asking.
	eventually(t, "debounced session write", func() bool {
		stored, err := m.st.GetSession("s")
		return err == nil && stored.LastMessage == "message 199"
	})
	if got := counter.sessions.Load(); got > 3 {
		t.Fatalf("debounce wrote %d times", got)
	}
}

func TestIdenticalHotEventPublishesNothing(t *testing.T) {
	m, pubs, _ := persistFixture(t)
	m.apply("s", Event{Kind: EvActivity, Text: "Thinking"})
	m.apply("s", Event{Kind: EvActivity, Text: "Thinking"})
	if n := len(pubs.all("agents.delta")); n != 1 {
		t.Fatalf("repeated identical activity published %d deltas", n)
	}
}

func TestStatusChangesWriteImmediatelyAndPublishTheFullSession(t *testing.T) {
	m, pubs, _ := persistFixture(t)
	m.apply("s", Event{Kind: EvMessage, Text: "pending in memory"})
	pubs.reset()
	m.apply("s", Event{Kind: EvTurnDone, Data: jraw(map[string]any{"ok": true, "result": "finished"})})
	stored, err := m.st.GetSession("s")
	if err != nil || stored.Status != StatusDone || stored.Result != "finished" || stored.LastMessage != "pending in memory" {
		t.Fatalf("status change not durable at once (and with the debounced fields): %+v %v", stored, err)
	}
	updates := pubs.all("agents.updated")
	if len(updates) == 0 {
		t.Fatal("status change did not publish agents.updated")
	}
	if got := updates[len(updates)-1].(Session); got.Status != StatusDone || !got.Live {
		t.Fatalf("published session = %+v", got)
	}
}

func TestHotEventThatChangesStatusTakesTheFullPath(t *testing.T) {
	m, pubs, _ := persistFixture(t)
	m.mu.Lock()
	m.live["s"].s.Status = StatusIdle
	m.mu.Unlock()
	m.apply("s", Event{Kind: EvWorking, Text: "turn started"})
	if len(pubs.all("agents.delta")) != 0 || len(pubs.all("agents.updated")) == 0 {
		t.Fatalf("status change by a hot event published deltas=%d updated=%d", len(pubs.all("agents.delta")), len(pubs.all("agents.updated")))
	}
	stored, _ := m.st.GetSession("s")
	if stored.Status != StatusWorking || stored.Activity != "turn started" {
		t.Fatalf("not written at once: %+v", stored)
	}
}

// A permission-mode change is not a delta field: it must reach SQLite at once (along with any
// debounced fields) and publish the whole session, whichever mutate path made it.
func TestPermissionModeChangeWritesImmediatelyAndPublishesTheFullSession(t *testing.T) {
	m, pubs, _ := persistFixture(t)
	m.apply("s", Event{Kind: EvMessage, Text: "pending in memory"})
	pubs.reset()
	m.mutateHot("s", func(s *Session) { s.PermissionMode = PermAuto })
	stored, err := m.st.GetSession("s")
	if err != nil || stored.PermissionMode != PermAuto || stored.LastMessage != "pending in memory" {
		t.Fatalf("permission mode not durable at once: %+v %v", stored, err)
	}
	if len(pubs.all("agents.delta")) != 0 {
		t.Fatal("permission mode change was published as a delta")
	}
	updates := pubs.all("agents.updated")
	if len(updates) != 1 || updates[0].(Session).PermissionMode != PermAuto {
		t.Fatalf("updates = %+v", updates)
	}
	if _, err := m.mutate("s", func(s *Session) { s.PermissionMode = PermYolo }); err != nil {
		t.Fatal(err)
	}
	if stored, _ = m.st.GetSession("s"); stored.PermissionMode != PermYolo {
		t.Fatalf("mutate not durable at once: %+v", stored)
	}
}

func TestShutdownFlushesPendingSessionAndEvents(t *testing.T) {
	m, _, _ := persistFixture(t)
	p := m.live["s"].proc.(*chatTestProcess)
	p.stopFn = nil // exit is reported below, as a CLI that ends on its own does
	for i := range 20 {
		m.apply("s", Event{Kind: EvMessage, Text: fmt.Sprintf("m%d", i)})
		m.apply("s", Event{Kind: EvActivity, Text: fmt.Sprintf("a%d", i)})
	}
	ls := m.live["s"]
	go func() { m.finalize(ls, Event{Kind: EvExit, Data: jraw(map[string]any{"code": 0})}); close(ls.done) }()
	<-ls.done
	m.Shutdown()
	stored, err := m.st.GetSession("s")
	if err != nil || stored.LastMessage != "m19" || stored.Status != StatusDone {
		t.Fatalf("clean exit lost state: %+v %v", stored, err)
	}
	var count int
	if err := m.st.db.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id='s'`).Scan(&count); err != nil || count < 41 {
		t.Fatalf("clean exit lost events: %d %v", count, err)
	}
}

func TestMutateHotAfterCloseWritesThrough(t *testing.T) {
	m, _, _ := persistFixture(t)
	if err := m.w.close(); err != nil {
		t.Fatal(err)
	}
	m.apply("s", Event{Kind: EvMessage, Text: "after close"})
	stored, _ := m.st.GetSession("s")
	if stored.LastMessage != "after close" {
		t.Fatalf("write after close was left in memory: %+v", stored)
	}
}

func TestPersistFailureRollsBackALiveMutation(t *testing.T) {
	m, pubs, _ := persistFixture(t)
	base := m.st.db
	m.st.db = chatFailDB{DB: base, fail: func(s Session) bool { return s.ReviewNote == "boom" }}
	if _, err := m.mutate("s", func(s *Session) { s.ReviewNote = "boom" }); err == nil {
		t.Fatal("persistence failure hidden")
	}
	if got, _ := m.Get("s"); got.ReviewNote != "" {
		t.Fatalf("memory ran ahead of storage: %+v", got)
	}
	if len(pubs.all("agents.updated")) != 0 {
		t.Fatal("failed change was published")
	}
	m.st.db = base
}

func TestEventsAreBatchedOrderedAndVisibleToReaders(t *testing.T) {
	m, _, counter := persistFixture(t)
	const n = 300
	var sent []Event
	for i := range n {
		sent = append(sent, m.record("s", Event{Kind: EvSystem, Text: fmt.Sprintf("e%d", i)}))
	}
	if got := counter.events.Load(); got > n/20 {
		t.Fatalf("%d events used %d insert statements", n, got)
	}
	got, err := m.Events("s", 0, 2000) // flushes the queue first
	if err != nil || len(got) != n {
		t.Fatalf("read %d of %d events: %v", len(got), n, err)
	}
	for i, e := range got {
		if e.ID != sent[i].ID || e.Text != sent[i].Text || (i > 0 && e.ID <= got[i-1].ID) {
			t.Fatalf("event %d: stored %+v, published %+v", i, e, sent[i])
		}
	}
}

func TestEventIDsContinueAfterRestart(t *testing.T) {
	m, _, _ := persistFixture(t)
	last := m.record("s", Event{Kind: EvSystem, Text: "before"})
	if err := m.st.FlushEvents(); err != nil {
		t.Fatal(err)
	}
	reopened := NewStore(m.st.db)
	e := Event{SessionID: "s", At: time.Now().UTC(), Kind: EvSystem, Text: "after"}
	if err := reopened.QueueEvent(&e); err != nil {
		t.Fatal(err)
	}
	if e.ID <= last.ID {
		t.Fatalf("id %d reused after restart (last %d)", e.ID, last.ID)
	}
	if err := reopened.FlushEvents(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSessionDropsQueuedEvents(t *testing.T) {
	m, _, _ := persistFixture(t)
	for range 5 {
		m.record("s", Event{Kind: EvSystem, Text: "queued"})
	}
	if err := m.st.DeleteSession("s"); err != nil {
		t.Fatal(err)
	}
	if err := m.st.FlushEvents(); err != nil {
		t.Fatalf("flush after delete: %v", err)
	}
	var count int
	if err := m.st.db.QueryRow(`SELECT COUNT(*) FROM agent_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("events outlived their session: %d %v", count, err)
	}
}

func TestEventPruningCountsPerSession(t *testing.T) {
	db := persistDB(t)
	st := NewStore(db)
	now := time.Now().UTC()
	for _, id := range []string{"busy", "quiet"} {
		if err := st.SaveSession(Session{ID: id, Kind: "claude", StartedAt: now, UpdatedAt: now}, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Ids of the busy session are interleaved with another session's, so no id-modulo trick
	// could land on its history reliably; its own counter must trigger the pruning.
	total := maxEventsPerSession + 2*pruneEvery
	for i := range total {
		e := Event{SessionID: "busy", At: now, Kind: EvSystem, Text: "x"}
		if err := st.QueueEvent(&e); err != nil {
			t.Fatal(err)
		}
		if i%1000 == 0 {
			q := Event{SessionID: "quiet", At: now, Kind: EvSystem, Text: "keep"}
			if err := st.QueueEvent(&q); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.FlushEvents(); err != nil {
		t.Fatal(err)
	}
	count := func(id string) (n int) {
		if err := db.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id=?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("busy"); n < maxEventsPerSession || n >= maxEventsPerSession+pruneEvery {
		t.Fatalf("busy session holds %d events; want between %d and %d", n, maxEventsPerSession, maxEventsPerSession+pruneEvery)
	}
	if n := count("quiet"); n != total/1000+1 {
		t.Fatalf("quiet session lost events: %d", n)
	}
}

func TestListSessionsPages(t *testing.T) {
	st := NewStore(persistDB(t))
	base := time.Now().UTC().Truncate(time.Second)
	for i := range 5 {
		at := base.Add(time.Duration(i) * time.Minute)
		if err := st.SaveSession(Session{ID: fmt.Sprintf("s%d", i), Kind: "claude", StartedAt: at, UpdatedAt: at}, ""); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(limit, offset int) string {
		t.Helper()
		page, err := st.ListSessions(limit, offset)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range page {
			out = append(out, s.ID)
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		limit, offset int
		want          string
	}{
		{0, 0, "s4,s3,s2,s1,s0"}, {2, 0, "s4,s3"}, {2, 2, "s2,s1"}, {2, 4, "s0"}, {2, 9, ""}, {-1, -3, "s4,s3,s2,s1,s0"},
	} {
		if got := ids(c.limit, c.offset); got != c.want {
			t.Fatalf("ListSessions(%d,%d) = %q, want %q", c.limit, c.offset, got, c.want)
		}
	}
}

func TestManagerListPageOverlaysLiveSessions(t *testing.T) {
	m, _, _ := persistFixture(t)
	m.apply("s", Event{Kind: EvMessage, Text: "only in memory so far"})
	page, err := m.ListPage(10, 0)
	if err != nil || len(page) != 1 || !page[0].Live || page[0].LastMessage != "only in memory so far" {
		t.Fatalf("page = %+v %v", page, err)
	}
}

func TestReadBoundedLine(t *testing.T) {
	long := strings.Repeat("x", 500)
	br := bufio.NewReaderSize(strings.NewReader("short\n"+long+"\n"+long[:99]+"\nlast"), 16)
	var got []string
	for {
		line, dropped, err := readBoundedLine(br, 100)
		switch {
		case dropped:
			got = append(got, "DROPPED")
		case len(line) > 0:
			got = append(got, string(line))
		}
		if err != nil {
			break
		}
	}
	want := []string{"short", "DROPPED", long[:99], "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// diffStatRecorder is a Worktrees that only answers diff statistics.
type diffStatRecorder struct {
	Worktrees
	mu          sync.Mutex
	calls       map[string]int
	running     int
	maxRunning  int
	release     chan struct{}
	started     chan string
	files, adds int
}

func (d *diffStatRecorder) DiffStat(ctx context.Context, dir, base string) (int, int, int, error) {
	d.mu.Lock()
	d.calls[dir]++
	d.running++
	d.maxRunning = max(d.maxRunning, d.running)
	files, adds := d.files, d.adds
	d.mu.Unlock()
	d.started <- dir
	<-d.release
	d.mu.Lock()
	d.running--
	d.mu.Unlock()
	return files, adds, 0, nil
}

func diffFixture(t *testing.T, ids ...string) (*Manager, *diffStatRecorder, *pubRecorder) {
	t.Helper()
	m, pubs, _ := persistFixture(t)
	rec := &diffStatRecorder{calls: map[string]int{}, release: make(chan struct{}), started: make(chan string, 64), files: 2, adds: 7}
	m.cfg.Worktrees = rec
	now := time.Now().UTC()
	m.mu.Lock()
	for _, id := range ids {
		s := Session{ID: id, Kind: "claude", Machine: "local", Mode: ModeStructured, Status: StatusDone, Dir: id, BaseRef: "base", StartedAt: now, UpdatedAt: now}
		m.live[id] = &liveSess{id: id, s: s, proc: &chatTestProcess{}, done: make(chan struct{})}
		if err := m.st.SaveSession(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Unlock()
	return m, rec, pubs
}

func TestDiffStatRefreshesCoalescePerSession(t *testing.T) {
	m, rec, pubs := diffFixture(t, "a")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); m.refreshDiffStat("a") }()
	<-rec.started // the first refresh is running
	for range 20 {
		m.refreshDiffStat("a") // returns at once: it only asks for one more run
	}
	rec.release <- struct{}{} // first run ends...
	<-rec.started             // ...and the single follow-up starts
	rec.release <- struct{}{}
	wg.Wait()
	rec.mu.Lock()
	calls := rec.calls["a"]
	rec.mu.Unlock()
	if calls != 2 {
		t.Fatalf("21 requests ran git %d times; want 2 (one in flight + one catch-up)", calls)
	}
	got, _ := m.Get("a")
	if got.Files != 2 || got.Additions != 7 {
		t.Fatalf("counts not recorded: %+v", got)
	}
	deltas := pubs.all("agents.delta")
	if len(deltas) != 1 || deltas[0].(SessionDelta).Additions == nil || *deltas[0].(SessionDelta).Additions != 7 {
		t.Fatalf("stat refresh published %+v", deltas)
	}
}

func TestDiffStatRefreshesAcrossSessionsAreBounded(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	m, rec, _ := diffFixture(t, ids...)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() { defer wg.Done(); m.refreshDiffStat(id) }()
	}
	for range diffStatWorkers {
		<-rec.started
	}
	time.Sleep(100 * time.Millisecond) // give any excess refresh the chance to start
	rec.mu.Lock()
	running := rec.running
	rec.mu.Unlock()
	if running != diffStatWorkers {
		t.Fatalf("%d refreshes running; want exactly %d", running, diffStatWorkers)
	}
	go func() {
		for range ids {
			rec.release <- struct{}{}
		}
	}()
	wg.Wait()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.maxRunning > diffStatWorkers {
		t.Fatalf("up to %d refreshes ran at once; limit is %d", rec.maxRunning, diffStatWorkers)
	}
	for _, id := range ids {
		if rec.calls[id] != 1 {
			t.Fatalf("session %s refreshed %d times", id, rec.calls[id])
		}
	}
}
