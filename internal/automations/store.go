package automations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("automation or run not found")

func (e *Engine) Save(ctx context.Context, d Definition) (SaveResult, error) {
	d = Normalize(Upgrade(d))
	issues := Validate(d)
	if len(issues) > 0 {
		return SaveResult{Definition: d, Issues: issues}, nil
	}
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	b, err := json.Marshal(d)
	if err != nil {
		return SaveResult{}, err
	}
	if len(b) > 256<<10 {
		return SaveResult{}, fmt.Errorf("definition exceeds 256 KiB")
	}
	var document any
	if err := json.Unmarshal(b, &document); err != nil {
		return SaveResult{}, err
	}
	canonical, _ := json.Marshal(document)
	redacted, _ := json.Marshal(e.redact.value(document))
	if !bytes.Equal(canonical, redacted) {
		return SaveResult{Issues: []Issue{{Path: "steps", Message: "Definitions cannot contain credentials or recognized secrets; use the integration's OS credential store"}}}, nil
	}
	now := e.clock.Now().UnixMilli()
	_, err = e.db.ExecContext(ctx, `INSERT INTO automations(id,name,enabled,schema_version,trigger_type,definition,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,schema_version=excluded.schema_version,trigger_type=excluded.trigger_type,definition=excluded.definition,updated_at=excluded.updated_at,last_fire_at=CASE WHEN definition=excluded.definition THEN last_fire_at ELSE 0 END`, d.ID, d.Name, d.Enabled, d.SchemaVersion, d.Trigger.Type, string(b), now, now)
	if err == nil {
		e.invalidateDefinitions()
		e.wake()
	}
	return SaveResult{Definition: d, Issues: issues, Saved: err == nil}, err
}
func (e *Engine) List(ctx context.Context) ([]Summary, error) {
	// Read the small watch table first so this call never holds two connections.
	watches, err := e.watchStatus(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := e.db.QueryContext(ctx, `SELECT a.definition,coalesce((SELECT status FROM automation_runs r WHERE r.automation_id=a.id ORDER BY created_at DESC LIMIT 1),''),coalesce((SELECT created_at FROM automation_runs r WHERE r.automation_id=a.id ORDER BY created_at DESC LIMIT 1),0) FROM automations a ORDER BY a.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var s Summary
		var b string
		if err := rows.Scan(&b, &s.LastStatus, &s.LastRunAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(b), &s.Definition); err != nil {
			return nil, err
		}
		s.Definition = Upgrade(s.Definition)
		s.Issues = Validate(s.Definition)
		if s.Definition.Enabled && s.Definition.Trigger.Type == "schedule" {
			if sc, err := ParseSchedule(s.Definition.Trigger.Schedule, nil); err == nil {
				if next, ok := sc.Next(e.clock.Now()); ok {
					s.NextRunAt = next.UnixMilli()
				}
			}
		}
		if w, ok := watches[s.Definition.ID]; ok && s.Definition.Trigger.Type == "watch" {
			s.Watch = &w
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (e *Engine) Delete(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var n int
	if err := e.db.QueryRowContext(ctx, `SELECT count(*) FROM automation_runs WHERE automation_id=? AND status IN ('queued','running','waiting-approval')`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("cancel active runs before deleting")
	}
	r, err := e.db.ExecContext(ctx, `DELETE FROM automations WHERE id=?`, id)
	if err == nil {
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		e.invalidateDefinitions()
		e.wake()
	}
	return err
}

const runColumns = `id,automation_id,status,trigger_type,trigger_data,chain,definition,error,created_at,started_at,finished_at`

type scanner interface{ Scan(...any) error }

func scanRun(s scanner) (Run, error) {
	var r Run
	var trigger, chain, definition string
	err := s.Scan(&r.ID, &r.AutomationID, &r.Status, &r.TriggerType, &trigger, &chain, &definition, &r.Error, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
	if err != nil {
		return r, err
	}
	r.TriggerData = json.RawMessage(trigger)
	if err = json.Unmarshal([]byte(chain), &r.Chain); err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(definition), &r.Definition); err == nil {
		r.Definition = Upgrade(r.Definition)
	}
	r.Name = r.Definition.Name
	return r, err
}
func (e *Engine) loadRun(ctx context.Context, id string) (Run, error) {
	r, err := scanRun(e.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM automation_runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}
func (e *Engine) Runs(ctx context.Context, id string) ([]Run, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT `+runColumns+` FROM automation_runs WHERE (?='' OR automation_id=?) ORDER BY created_at DESC LIMIT 200`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (e *Engine) Detail(ctx context.Context, id string) (Detail, error) {
	r, err := e.loadRun(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	steps, err := e.loadJournals(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Run: r, Steps: steps}, nil
}
func (e *Engine) loadJournals(ctx context.Context, id string) ([]Journal, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT run_id,step_id,seq,step_type,status,attempt,idempotency_key,input,output,error,started_at,finished_at FROM automation_steps_journal WHERE run_id=? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Journal{}
	for rows.Next() {
		var j Journal
		var in, output string
		if err := rows.Scan(&j.RunID, &j.StepID, &j.Seq, &j.Type, &j.Status, &j.Attempt, &j.Key, &in, &output, &j.Error, &j.StartedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		j.Input = json.RawMessage(in)
		j.Output = json.RawMessage(output)
		out = append(out, j)
	}
	return out, rows.Err()
}
func (e *Engine) journal(j Journal) error {
	if len(j.Input) == 0 {
		j.Input = json.RawMessage(`null`)
	}
	if len(j.Output) == 0 {
		j.Output = json.RawMessage(`null`)
	}
	j.Input = e.redact.JSON(j.Input)
	j.Output = e.redact.JSON(j.Output)
	j.Error = e.redact.Text(j.Error)
	_, err := e.db.Exec(`INSERT INTO automation_steps_journal(run_id,step_id,seq,step_type,status,attempt,idempotency_key,input,output,error,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(run_id,step_id) DO UPDATE SET status=excluded.status,attempt=excluded.attempt,input=excluded.input,output=excluded.output,error=excluded.error,started_at=excluded.started_at,finished_at=excluded.finished_at`, j.RunID, j.StepID, j.Seq, j.Type, j.Status, j.Attempt, j.Key, string(j.Input), string(j.Output), j.Error, j.StartedAt, j.FinishedAt)
	if err == nil {
		e.changed(j.RunID)
	}
	return err
}
func (e *Engine) status(id, status, message string) error {
	now := e.clock.Now().UnixMilli()
	finish := int64(0)
	if terminal(status) {
		finish = now
	}
	message = e.redact.Text(message)
	res, err := e.db.Exec(`UPDATE automation_runs SET status=?,error=?,started_at=CASE WHEN started_at=0 AND ?='running' THEN ? ELSE started_at END,finished_at=? WHERE id=? AND status NOT IN ('succeeded','failed','canceled','uncertain')`, status, message, status, now, finish, id)
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil
	}
	r, err := e.applyStatus(id, status, message, now, finish)
	if err != nil {
		return nil // the row was committed; only the summary event is lost
	}
	if e.changedFn != nil {
		e.changedFn(r)
	}
	if terminal(status) {
		e.audit("automation.run.finish", id, status, message)
		e.finished(r)
	}
	return nil
}
func terminal(s string) bool {
	return s == "succeeded" || s == "failed" || s == "canceled" || s == "uncertain"
}
func (e *Engine) Approvals(ctx context.Context) ([]Approval, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT j.run_id,j.step_id,a.name,j.input FROM automation_steps_journal j JOIN automation_runs r ON r.id=j.run_id JOIN automations a ON a.id=r.automation_id WHERE j.status='waiting-approval' AND r.status='waiting-approval' ORDER BY j.started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		var a Approval
		var b string
		if err := rows.Scan(&a.RunID, &a.StepID, &a.Name, &b); err != nil {
			return nil, err
		}
		var m struct {
			Message   string `json:"message"`
			ExpiresAt int64  `json:"expiresAt"`
		}
		if err := json.Unmarshal([]byte(b), &m); err != nil {
			return nil, err
		}
		a.Message = m.Message
		a.ExpiresAt = m.ExpiresAt
		out = append(out, a)
	}
	return out, rows.Err()
}
func (e *Engine) Approve(ctx context.Context, runID, stepID string, approved bool, note string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := "failed"
	msg := "Approval rejected"
	if approved {
		s = "succeeded"
		msg = ""
	}
	b, _ := json.Marshal(map[string]any{"approved": approved, "note": e.redact.Text(note)})
	res, err := tx.ExecContext(ctx, `UPDATE automation_steps_journal SET status=?,output=?,error=?,finished_at=? WHERE run_id=? AND step_id=? AND status='waiting-approval' AND EXISTS(SELECT 1 FROM automation_runs WHERE id=? AND status='waiting-approval')`, s, string(b), msg, e.clock.Now().UnixMilli(), runID, stepID, runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("approval is no longer pending")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE automation_runs SET status='queued' WHERE id=?`, runID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	e.audit("automation.approval", runID, s, note)
	e.markQueued(runID)
	e.queue = append(e.queue, runID)
	e.pumpLocked()
	return nil
}
func (e *Engine) expireApprovals() {
	list, err := e.Approvals(context.Background())
	if err != nil {
		return
	}
	now := e.clock.Now().UnixMilli()
	for _, a := range list {
		r, err := e.runState(context.Background(), a.RunID)
		if err == nil && r.StartedAt > 0 && now-r.StartedAt >= int64(r.Definition.TimeoutSec)*1000 {
			_ = e.Approve(context.Background(), a.RunID, a.StepID, false, "Run deadline exceeded")
			continue
		}
		if a.ExpiresAt > 0 && a.ExpiresAt <= now {
			_ = e.Approve(context.Background(), a.RunID, a.StepID, false, "Approval expired")
		}
	}
}
