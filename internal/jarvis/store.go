package jarvis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("jarvis: not found")

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

func validateRule(r Rule) error {
	if r.BotID == "" {
		return errors.New("A rule needs a bot.")
	}
	if !r.Trigger.Valid() {
		return fmt.Errorf("Unknown trigger %q.", r.Trigger)
	}
	if r.Trigger == AtTime && !hhmm.MatchString(r.Target) {
		return errors.New("A time rule needs a time like 08:30.")
	}
	if r.Trigger != AtTime && r.Target == "" {
		return errors.New("A rule needs a target id or \"any\".")
	}
	if len(r.Target) > 200 || len(r.Note) > 280 {
		return errors.New("The rule's target or note is too long.")
	}
	return nil
}

func (s *Store) AddRule(ctx context.Context, r Rule) (Rule, error) {
	if err := validateRule(r); err != nil {
		return Rule{}, err
	}
	r.ID = uuid.NewString()
	r.CreatedAt = s.now().UnixMilli()
	var exp any
	if r.ExpiresAt > 0 {
		exp = r.ExpiresAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO jarvis_rules(id,bot_id,trigger,target,repeat,urgent,quiet_override,note,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, r.ID, r.BotID, string(r.Trigger), r.Target, r.Repeat, r.Urgent, r.QuietOverride, r.Note, exp, r.CreatedAt)
	return r, err
}

func (s *Store) Rules(ctx context.Context, botID string) ([]Rule, error) {
	q := `SELECT id,bot_id,trigger,target,repeat,urgent,quiet_override,note,COALESCE(expires_at,0),created_at FROM jarvis_rules`
	args := []any{}
	if botID != "" {
		q += ` WHERE bot_id=?`
		args = append(args, botID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var trig string
		if err := rows.Scan(&r.ID, &r.BotID, &trig, &r.Target, &r.Repeat, &r.Urgent, &r.QuietOverride, &r.Note, &r.ExpiresAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Trigger = Trigger(trig)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jarvis_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const callCols = `id,COALESCE(bot_id,''),direction,COALESCE(rule_id,''),reason,status,provider_call_id,conversation_id,message_id,
	COALESCE(started_at,0),COALESCE(answered_at,0),COALESCE(ended_at,0),est_cost_micros,created_at`

func scanCall(sc interface{ Scan(...any) error }) (Call, error) {
	var c Call
	err := sc.Scan(&c.ID, &c.BotID, &c.Direction, &c.RuleID, &c.Reason, &c.Status, &c.ProviderCallID, &c.ConversationID,
		&c.MessageID, &c.StartedAt, &c.AnsweredAt, &c.EndedAt, &c.EstCostMicros, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (s *Store) InsertCall(ctx context.Context, c *Call) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.CreatedAt = s.now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO jarvis_calls(id,bot_id,direction,rule_id,reason,status,provider_call_id,conversation_id,message_id,started_at,answered_at,ended_at,est_cost_micros,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, nullStr(c.BotID), c.Direction, nullStr(c.RuleID), c.Reason, c.Status, c.ProviderCallID,
		c.ConversationID, c.MessageID, nullInt(c.StartedAt), nullInt(c.AnsweredAt), nullInt(c.EndedAt), c.EstCostMicros, c.CreatedAt)
	return err
}

func (s *Store) UpdateCall(ctx context.Context, c Call) error {
	res, err := s.db.ExecContext(ctx, `UPDATE jarvis_calls SET bot_id=?,status=?,provider_call_id=?,conversation_id=?,message_id=?,
		started_at=?,answered_at=?,ended_at=?,est_cost_micros=? WHERE id=?`, nullStr(c.BotID), c.Status, c.ProviderCallID, c.ConversationID,
		c.MessageID, nullInt(c.StartedAt), nullInt(c.AnsweredAt), nullInt(c.EndedAt), c.EstCostMicros, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetCall(ctx context.Context, id string) (Call, error) {
	return scanCall(s.db.QueryRowContext(ctx, `SELECT `+callCols+` FROM jarvis_calls WHERE id=?`, id))
}

func (s *Store) RecentCalls(ctx context.Context, botID string, limit int) ([]Call, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+callCols+` FROM jarvis_calls WHERE (?='' OR bot_id=?) ORDER BY created_at DESC LIMIT ?`, botID, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CallsSince counts placed calls (inbound and outbound, any status except rejected) created at or after ms.
func (s *Store) CallsSince(ctx context.Context, ms int64) (int, int64, error) {
	var n int
	var cost int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(est_cost_micros),0) FROM jarvis_calls WHERE created_at>=? AND status<>?`,
		ms, StatusRejected).Scan(&n, &cost)
	return n, cost, err
}

func (s *Store) LastCallAt(ctx context.Context, botID string) (int64, error) {
	var at sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM jarvis_calls WHERE bot_id=? AND direction=?`, botID, Outbound).Scan(&at)
	return at.Int64, err
}
