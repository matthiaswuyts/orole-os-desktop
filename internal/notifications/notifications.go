// Package notifications persists the notification center: what happened, from
// which source, and whether the user has seen or dismissed it. It is stateless
// over the daemon database; rendering and OS toasts belong to the desktop.
//
// Notification text can originate from untrusted content (mail subjects, issue
// titles, MCP results); consumers must render it as plain text.
package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Level is a notification's severity.
type Level string

// Levels.
const (
	Info    Level = "info"
	Success Level = "success"
	Warning Level = "warning"
	Error   Level = "error"
)

// Limits.
const (
	MaxTitle    = 200
	MaxBody     = 2000
	MaxSource   = 64
	MaxKeep     = 1000 // rows kept; oldest read-or-dismissed rows go first
	MaxList     = 500
	DefaultList = 100
	// PrefsKey is the settings key holding Prefs.
	PrefsKey = "notifications.prefs"
)

// ErrInvalid marks rejected input.
var ErrInvalid = errors.New("invalid notification")

// ErrNotFound is returned for unknown ids.
var ErrNotFound = errors.New("notification not found")

var sourceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*$`)

// Notification is one stored entry.
type Notification struct {
	ID        string `json:"id"`
	At        int64  `json:"at"` // unix seconds
	Source    string `json:"source"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Level     Level  `json:"level"`
	Read      bool   `json:"read"`
	Dismissed bool   `json:"dismissed"`
}

// SourcePref is the preference for one source. A missing entry means defaults.
type SourcePref struct {
	// Enabled false drops the source's notifications entirely.
	Enabled bool `json:"enabled"`
	// Native shows an OS notification as well.
	Native bool `json:"native"`
	// Sound allows sound for new notifications; Windows controls native volume.
	Sound bool `json:"sound"`
}

// UnmarshalJSON preserves audible behavior for preferences saved before Sound
// existed, including per-source map entries, without overriding explicit false.
func (p *SourcePref) UnmarshalJSON(data []byte) error {
	type sourcePref SourcePref
	value := sourcePref(*p)
	value.Sound = true
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*p = SourcePref(value)
	return nil
}

// Prefs are per-source preferences with defaults for unlisted sources.
type Prefs struct {
	Default SourcePref            `json:"default"`
	Sources map[string]SourcePref `json:"sources"`
}

// DefaultPrefs: every source enabled, audible OS toasts on.
func DefaultPrefs() Prefs {
	return Prefs{Default: SourcePref{Enabled: true, Native: true, Sound: true}, Sources: map[string]SourcePref{}}
}

// For returns the effective preference of a source.
func (p Prefs) For(source string) SourcePref {
	if s, ok := p.Sources[source]; ok {
		return s
	}
	return p.Default
}

// Store is the notification table.
type Store struct{ DB *sql.DB }

// New returns a Store over db.
func New(db *sql.DB) *Store { return &Store{DB: db} }

// Validate normalises and checks an input notification.
func Validate(source, title, body string, level Level) (Level, error) {
	if !sourceRe.MatchString(source) || len(source) > MaxSource {
		return "", fmt.Errorf("%w: source must be lower-case letters, digits and . _ : / - (max %d)", ErrInvalid, MaxSource)
	}
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > MaxTitle {
		return "", fmt.Errorf("%w: title is required (max %d characters)", ErrInvalid, MaxTitle)
	}
	if utf8.RuneCountInString(body) > MaxBody {
		return "", fmt.Errorf("%w: body too long (max %d characters)", ErrInvalid, MaxBody)
	}
	switch level {
	case "":
		level = Info
	case Info, Success, Warning, Error:
	default:
		return "", fmt.Errorf("%w: unknown level %q", ErrInvalid, level)
	}
	return level, nil
}

// Prefs loads preferences from the settings table.
func (s *Store) Prefs(ctx context.Context) (Prefs, error) {
	p := DefaultPrefs()
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, PrefsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return p, fmt.Errorf("decode notification preferences: %w", err)
	}
	return p, nil
}

// SetPrefs stores preferences.
func (s *Store) SetPrefs(ctx context.Context, p Prefs) error {
	if p.Sources == nil {
		p.Sources = map[string]SourcePref{}
	}
	if len(p.Sources) > 200 {
		return fmt.Errorf("%w: too many sources", ErrInvalid)
	}
	for k := range p.Sources {
		if !sourceRe.MatchString(k) || len(k) > MaxSource {
			return fmt.Errorf("%w: bad source id %q", ErrInvalid, k)
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		PrefsKey, string(b), time.Now().Unix())
	return err
}

// Result of Notify.
type Result struct {
	Notification Notification `json:"notification"`
	// Stored is false when the source is disabled by preference.
	Stored bool `json:"stored"`
	// Native tells the desktop to also show an OS notification.
	Native bool `json:"native"`
	// Sound applies only to this newly created notification, never history.
	Sound bool `json:"sound"`
}

// Notify validates, applies the source's preference, and persists.
func (s *Store) Notify(ctx context.Context, source, title, body string, level Level) (Result, error) {
	level, err := Validate(source, title, body, level)
	if err != nil {
		return Result{}, err
	}
	prefs, err := s.Prefs(ctx)
	if err != nil {
		return Result{}, err
	}
	pref := prefs.For(source)
	n := Notification{ID: uuid.NewString(), At: time.Now().Unix(), Source: source, Title: strings.TrimSpace(title), Body: body, Level: level}
	if !pref.Enabled {
		return Result{Notification: n}, nil
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO notifications(id,at,source,title,body,level) VALUES(?,?,?,?,?,?)`,
		n.ID, n.At, n.Source, n.Title, n.Body, string(n.Level)); err != nil {
		return Result{}, err
	}
	s.prune(ctx)
	return Result{Notification: n, Stored: true, Native: pref.Native, Sound: pref.Sound}, nil
}

func (s *Store) prune(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM notifications WHERE id IN (
		SELECT id FROM notifications ORDER BY (read=1 OR dismissed=1) DESC, at ASC LIMIT max(0, (SELECT COUNT(*) FROM notifications) - ?))`, MaxKeep)
}

// ListOptions filters List.
type ListOptions struct {
	Source           string `json:"source,omitempty"`
	UnreadOnly       bool   `json:"unreadOnly,omitempty"`
	IncludeDismissed bool   `json:"includeDismissed,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

// List returns newest first.
func (s *Store) List(ctx context.Context, o ListOptions) ([]Notification, error) {
	q := `SELECT id,at,source,title,body,level,read,dismissed FROM notifications WHERE 1=1`
	var args []any
	if !o.IncludeDismissed {
		q += ` AND dismissed=0`
	}
	if o.UnreadOnly {
		q += ` AND read=0`
	}
	if o.Source != "" {
		q += ` AND source=?`
		args = append(args, o.Source)
	}
	limit := o.Limit
	if limit <= 0 {
		limit = DefaultList
	}
	if limit > MaxList {
		limit = MaxList
	}
	q += ` ORDER BY at DESC, rowid DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var lvl string
		var r, d int
		if err := rows.Scan(&n.ID, &n.At, &n.Source, &n.Title, &n.Body, &lvl, &r, &d); err != nil {
			return nil, err
		}
		n.Level, n.Read, n.Dismissed = Level(lvl), r != 0, d != 0
		out = append(out, n)
	}
	return out, rows.Err()
}

// Unread counts visible unread notifications.
func (s *Store) Unread(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE read=0 AND dismissed=0`).Scan(&n)
	return n, err
}

// Sources lists sources that have visible notifications, with counts.
type SourceCount struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
	Unread int    `json:"unread"`
}

// Sources returns per-source counts of non-dismissed notifications.
func (s *Store) Sources(ctx context.Context) ([]SourceCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT source, COUNT(*), SUM(read=0) FROM notifications WHERE dismissed=0 GROUP BY source ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceCount{}
	for rows.Next() {
		var c SourceCount
		if err := rows.Scan(&c.Source, &c.Count, &c.Unread); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func (s *Store) update(ctx context.Context, col string, ids []string, source string, all bool) (int64, error) {
	var res sql.Result
	var err error
	switch {
	case all:
		q := `UPDATE notifications SET ` + col + `=1 WHERE ` + col + `=0`
		if source != "" {
			res, err = s.DB.ExecContext(ctx, q+` AND source=?`, source)
		} else {
			res, err = s.DB.ExecContext(ctx, q)
		}
	case len(ids) > 0:
		if len(ids) > 1000 {
			return 0, fmt.Errorf("%w: too many ids", ErrInvalid)
		}
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		res, err = s.DB.ExecContext(ctx, `UPDATE notifications SET `+col+`=1 WHERE `+col+`=0 AND id IN (`+placeholders(len(ids))+`)`, args...)
	default:
		return 0, fmt.Errorf("%w: give ids or all", ErrInvalid)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkRead marks ids (or all, optionally limited to a source) read.
func (s *Store) MarkRead(ctx context.Context, ids []string, source string, all bool) (int64, error) {
	return s.update(ctx, "read", ids, source, all)
}

// Dismiss hides ids (or all, optionally limited to a source). Dismissed
// notifications also count as read.
func (s *Store) Dismiss(ctx context.Context, ids []string, source string, all bool) (int64, error) {
	n, err := s.update(ctx, "dismissed", ids, source, all)
	if err != nil {
		return n, err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE notifications SET read=1 WHERE dismissed=1 AND read=0`)
	return n, err
}
