package agents

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DB is the subset of *database.DB the store needs.
type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// ErrNotFound is returned for unknown session ids.
var ErrNotFound = errors.New("agents: no such session")

// maxEventsPerSession bounds the history kept per session; the oldest events are pruned.
const maxEventsPerSession = 5000

// Store persists sessions, events, journal rows and runs.
type Store struct{ db DB }

// NewStore wraps db.
func NewStore(db DB) *Store { return &Store{db: db} }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// SaveSession inserts or updates s. Runtime-only fields are never stored.
func (st *Store) SaveSession(s Session, key string) error {
	s.Live, s.Pending, s.Questions = false, nil, nil
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var k any
	if key != "" {
		k = key
	}
	_, err = st.db.Exec(`INSERT INTO agent_sessions(id,kind,status,project,dir,parent_id,grp,journal_key,started_at,ended_at,updated_at,pr_url,pr_number,data)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, project=excluded.project, dir=excluded.dir,
			ended_at=excluded.ended_at, updated_at=excluded.updated_at, pr_url=excluded.pr_url, pr_number=excluded.pr_number, data=excluded.data`,
		s.ID, s.Kind, string(s.Status), s.Project, s.Dir, s.ParentID, s.Group, k, s.StartedAt.Unix(), nullTime(s.EndedAt), s.UpdatedAt.Unix(), s.PRURL, s.PRNumber, string(b))
	return err
}

func scanSession(sc interface{ Scan(...any) error }) (Session, error) {
	var data string
	if err := sc.Scan(&data); err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return Session{}, err
	}
	// Never resurrect requests from legacy or externally restored JSON.
	s.Live, s.Pending, s.Questions = false, nil, nil
	if s.Machine == "" {
		s.Machine = "local"
	}
	if s.Pending == nil {
		s.Pending = []PermissionRequest{}
	}
	return s, nil
}

// GetSession loads one session.
func (st *Store) GetSession(id string) (Session, error) {
	s, err := scanSession(st.db.QueryRow(`SELECT data FROM agent_sessions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return s, err
}

// SessionByKey finds the session started with an idempotency key.
func (st *Store) SessionByKey(key string) (Session, bool, error) {
	s, err := scanSession(st.db.QueryRow(`SELECT data FROM agent_sessions WHERE journal_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	return s, err == nil, err
}

// ListSessions returns every session, newest first.
func (st *Store) ListSessions() ([]Session, error) {
	rows, err := st.db.Query(`SELECT data FROM agent_sessions ORDER BY started_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSession removes a session and (by cascade) its events.
func (st *Store) DeleteSession(id string) error {
	_, err := st.db.Exec(`DELETE FROM agent_sessions WHERE id=?`, id)
	return err
}

// AppendEvent stores e (assigning e.ID) and prunes old history.
func (st *Store) AppendEvent(e *Event) error {
	data := "null"
	if len(e.Data) > 0 {
		data = string(e.Data)
	}
	res, err := st.db.Exec(`INSERT INTO agent_events(session_id,at,kind,text,data) VALUES(?,?,?,?,?)`,
		e.SessionID, e.At.UnixMilli(), e.Kind, e.Text, data)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	if e.ID%256 == 0 { // amortised pruning
		_, _ = st.db.Exec(`DELETE FROM agent_events WHERE session_id=? AND id <= (
			SELECT id FROM agent_events WHERE session_id=? ORDER BY id DESC LIMIT 1 OFFSET ?)`,
			e.SessionID, e.SessionID, maxEventsPerSession)
	}
	return nil
}

// Events returns events with id > after, oldest first. kinds filters when non-empty.
func (st *Store) Events(sessionID string, after int64, limit int, kinds ...string) ([]Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	q := `SELECT id,session_id,at,kind,text,data FROM agent_events WHERE session_id=? AND id>?`
	args := []any{sessionID, after}
	if len(kinds) > 0 {
		q += ` AND kind IN (` + placeholders(len(kinds)) + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit)
	return st.queryEvents(q, args...)
}

// LastEvents returns the newest limit events, oldest first.
func (st *Store) LastEvents(sessionID string, limit int, kinds ...string) ([]Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	q := `SELECT id,session_id,at,kind,text,data FROM agent_events WHERE session_id=?`
	args := []any{sessionID}
	if len(kinds) > 0 {
		q += ` AND kind IN (` + placeholders(len(kinds)) + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	evs, err := st.queryEvents(q, args...)
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	return evs, err
}

func placeholders(n int) string {
	s := make([]byte, 0, n*2)
	for i := range n {
		if i > 0 {
			s = append(s, ',')
		}
		s = append(s, '?')
	}
	return string(s)
}

func (st *Store) queryEvents(q string, args ...any) ([]Event, error) {
	rows, err := st.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var ms int64
		var data string
		if err := rows.Scan(&e.ID, &e.SessionID, &ms, &e.Kind, &e.Text, &data); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(ms).UTC()
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// JournalEntry is one at-most-once operation.
type JournalEntry struct {
	Key       string
	Op        string
	Status    string
	Boot      string
	Result    json.RawMessage
	Error     string
	SessionID string
}

// Journal statuses.
const (
	JournalPending     = "pending"
	JournalDone        = "done"
	JournalFailed      = "failed"
	JournalInterrupted = "interrupted"
)

// JournalBegin records a pending op unless key exists. created reports whether this call
// inserted the row; otherwise the existing row is returned.
func (st *Store) JournalBegin(key, op string, params any, boot string) (e JournalEntry, created bool, err error) {
	pb, err := json.Marshal(params)
	if err != nil {
		return e, false, err
	}
	now := time.Now().Unix()
	res, err := st.db.Exec(`INSERT OR IGNORE INTO agent_journal(key,op,params,status,boot,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		key, op, string(pb), JournalPending, boot, now, now)
	if err != nil {
		return e, false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return JournalEntry{Key: key, Op: op, Status: JournalPending, Boot: boot}, true, nil
	}
	e, err = st.Journal(key)
	return e, false, err
}

// Journal loads a journal row.
func (st *Store) Journal(key string) (JournalEntry, error) {
	var e JournalEntry
	var res string
	err := st.db.QueryRow(`SELECT key,op,status,boot,result,error,session_id FROM agent_journal WHERE key=?`, key).
		Scan(&e.Key, &e.Op, &e.Status, &e.Boot, &res, &e.Error, &e.SessionID)
	e.Result = json.RawMessage(res)
	return e, err
}

// JournalFinish completes a row.
func (st *Store) JournalFinish(key, status string, result any, errText, sessionID string) error {
	rb, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(`UPDATE agent_journal SET status=?, result=?, error=?, session_id=?, updated_at=? WHERE key=?`,
		status, string(rb), errText, sessionID, time.Now().Unix(), key)
	return err
}

// Run is a long-running orchestration.
type Run struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`
	Status    string          `json:"status"`
	Params    json.RawMessage `json:"params"`
	State     json.RawMessage `json:"state"`
	Error     string          `json:"error"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Run statuses.
const (
	RunRunning     = "running"
	RunSucceeded   = "succeeded"
	RunFailed      = "failed"
	RunCancelled   = "cancelled"
	RunInterrupted = "interrupted"
)

// SaveRun upserts a run.
func (st *Store) SaveRun(r Run) error {
	if len(r.Params) == 0 {
		r.Params = json.RawMessage("null")
	}
	if len(r.State) == 0 {
		r.State = json.RawMessage("null")
	}
	now := time.Now().Unix()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Unix(now, 0)
	}
	_, err := st.db.Exec(`INSERT INTO agent_runs(id,kind,key,status,params,state,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, state=excluded.state, error=excluded.error, updated_at=excluded.updated_at`,
		r.ID, r.Kind, r.Key, r.Status, string(r.Params), string(r.State), r.Error, r.CreatedAt.Unix(), now)
	return err
}

// ListRuns returns recent runs, newest first.
func (st *Store) ListRuns(limit int) ([]Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := st.db.Query(`SELECT id,kind,key,status,params,state,error,created_at,updated_at FROM agent_runs ORDER BY created_at DESC, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		var p, s string
		var c, u int64
		if err := rows.Scan(&r.ID, &r.Kind, &r.Key, &r.Status, &p, &s, &r.Error, &c, &u); err != nil {
			return nil, err
		}
		r.Params, r.State = json.RawMessage(p), json.RawMessage(s)
		r.CreatedAt, r.UpdatedAt = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkInterrupted flags leftover pending journal rows and running runs from a previous daemon
// run; it never re-executes them.
func (st *Store) MarkInterrupted(currentBoot string) (journal, runs int64, err error) {
	res, err := st.db.Exec(`UPDATE agent_journal SET status=?, error='interrupted by daemon restart before completion; outcome unknown', updated_at=? WHERE status=? AND boot<>?`,
		JournalInterrupted, time.Now().Unix(), JournalPending, currentBoot)
	if err != nil {
		return 0, 0, err
	}
	journal, _ = res.RowsAffected()
	res, err = st.db.Exec(`UPDATE agent_runs SET status=?, error='interrupted by daemon restart', updated_at=? WHERE status=?`,
		RunInterrupted, time.Now().Unix(), RunRunning)
	if err != nil {
		return journal, 0, fmt.Errorf("runs: %w", err)
	}
	runs, _ = res.RowsAffected()
	return journal, runs, nil
}
