package notifications

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

var ctx = context.Background()

func open(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.Open(path, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNotifyPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orole.db")
	db := open(t, path)
	s := New(db.DB)
	r, err := s.Notify(ctx, "github", "PR merged", "details", Success)
	if err != nil || !r.Stored || !r.Native || !r.Sound {
		t.Fatalf("notify: %+v %v", r, err)
	}
	if _, err := s.Notify(ctx, "mail", "New mail", "", ""); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s2 := New(open(t, path).DB)
	list, err := s2.List(ctx, ListOptions{})
	if err != nil || len(list) != 2 {
		t.Fatalf("after reopen: %+v %v", list, err)
	}
	if list[0].Source != "mail" || list[0].Level != Info || list[1].Level != Success || list[1].Body != "details" || list[1].Read {
		t.Fatalf("rows: %+v", list)
	}
	if n, _ := s2.Unread(ctx); n != 2 {
		t.Fatalf("unread %d", n)
	}
}

func TestReadDismissFilter(t *testing.T) {
	s := New(open(t, filepath.Join(t.TempDir(), "orole.db")).DB)
	var ids []string
	for _, src := range []string{"a", "a", "b"} {
		r, err := s.Notify(ctx, src, "t-"+src, "", Info)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Notification.ID)
	}
	if n, err := s.MarkRead(ctx, ids[:1], "", false); err != nil || n != 1 {
		t.Fatalf("markread: %d %v", n, err)
	}
	if n, _ := s.MarkRead(ctx, ids[:1], "", false); n != 0 {
		t.Fatalf("re-marking counts again: %d", n)
	}
	if u, _ := s.Unread(ctx); u != 2 {
		t.Fatalf("unread %d", u)
	}
	if l, _ := s.List(ctx, ListOptions{Source: "a"}); len(l) != 2 {
		t.Fatalf("source filter: %d", len(l))
	}
	if l, _ := s.List(ctx, ListOptions{UnreadOnly: true}); len(l) != 2 {
		t.Fatalf("unread filter: %d", len(l))
	}
	srcs, _ := s.Sources(ctx)
	if len(srcs) != 2 || srcs[0].Source != "a" || srcs[0].Count != 2 || srcs[0].Unread != 1 {
		t.Fatalf("sources: %+v", srcs)
	}
	if n, err := s.Dismiss(ctx, ids[1:2], "", false); err != nil || n != 1 {
		t.Fatalf("dismiss: %d %v", n, err)
	}
	if l, _ := s.List(ctx, ListOptions{}); len(l) != 2 {
		t.Fatalf("dismissed still listed: %d", len(l))
	}
	if l, _ := s.List(ctx, ListOptions{IncludeDismissed: true}); len(l) != 3 {
		t.Fatalf("includeDismissed: %d", len(l))
	}
	if u, _ := s.Unread(ctx); u != 1 { // dismissed counts as read
		t.Fatalf("unread after dismiss %d", u)
	}
	if n, _ := s.Dismiss(ctx, nil, "b", true); n != 1 {
		t.Fatalf("dismiss all b: %d", n)
	}
	if _, err := s.MarkRead(ctx, nil, "", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty selection: %v", err)
	}
	if n, _ := s.MarkRead(ctx, nil, "", true); n != 0 {
		t.Fatalf("all: %d", n) // only the dismissed/read remain
	}
}

func TestValidation(t *testing.T) {
	s := New(open(t, filepath.Join(t.TempDir(), "orole.db")).DB)
	bad := []struct {
		src, title, body string
		lvl              Level
	}{
		{"", "t", "", Info}, {"Has Space", "t", "", Info}, {"UPPER", "t", "", Info}, {strings.Repeat("a", 65), "t", "", Info},
		{"a", "", "", Info}, {"a", "   ", "", Info}, {"a", strings.Repeat("x", MaxTitle+1), "", Info},
		{"a", "t", strings.Repeat("x", MaxBody+1), Info}, {"a", "t", "", "fatal"},
	}
	for i, b := range bad {
		if _, err := s.Notify(ctx, b.src, b.title, b.body, b.lvl); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d: want ErrInvalid, got %v", i, err)
		}
	}
	if l, _ := s.List(ctx, ListOptions{}); len(l) != 0 {
		t.Fatalf("invalid input was stored: %d", len(l))
	}
}

func TestPreferencesPerSource(t *testing.T) {
	s := New(open(t, filepath.Join(t.TempDir(), "orole.db")).DB)
	p, _ := s.Prefs(ctx)
	if !p.Default.Enabled || !p.Default.Native || !p.Default.Sound {
		t.Fatalf("defaults: %+v", p)
	}
	if err := s.SetPrefs(ctx, Prefs{Default: SourcePref{Enabled: true, Native: false, Sound: true}, Sources: map[string]SourcePref{
		"mail": {Enabled: false, Native: true, Sound: true}, "github": {Enabled: true, Native: true, Sound: false},
	}}); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Notify(ctx, "mail", "ignored", "", Info)
	if r.Stored || r.Native || r.Sound {
		t.Fatal("disabled source stored or requested native delivery/sound")
	}
	r, _ = s.Notify(ctx, "github", "kept", "", Info)
	if !r.Stored || !r.Native || r.Sound {
		t.Fatalf("github: %+v", r)
	}
	r, _ = s.Notify(ctx, "agents", "kept quietly", "", Info)
	if !r.Stored || r.Native || !r.Sound {
		t.Fatalf("default pref: %+v", r)
	}
	if l, _ := s.List(ctx, ListOptions{}); len(l) != 2 {
		t.Fatalf("stored: %d", len(l))
	}
	if err := s.SetPrefs(ctx, Prefs{Sources: map[string]SourcePref{"Bad Source": {}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad pref key: %v", err)
	}
	// Corrupt persisted preferences are reported, not silently replaced.
	if _, err := s.DB.Exec(`UPDATE settings SET value='{nope' WHERE key=?`, PrefsKey); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prefs(ctx); err == nil {
		t.Fatal("corrupt preferences were accepted")
	}
}

func TestSoundPreferencesPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orole.db")
	db := open(t, path)
	prefs := Prefs{
		Default: SourcePref{Enabled: true, Native: true, Sound: false},
		Sources: map[string]SourcePref{
			"audible": {Enabled: true, Native: true, Sound: true},
			"silent":  {Enabled: true, Native: true, Sound: false},
			"in-app":  {Enabled: true, Native: false, Sound: true},
			"muted":   {Enabled: false, Native: true, Sound: true},
		},
	}
	if err := New(db.DB).SetPrefs(ctx, prefs); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := New(open(t, path).DB)
	got, err := s.Prefs(ctx)
	if err != nil || got.Default != prefs.Default {
		t.Fatalf("reopened defaults: %+v, %v", got, err)
	}
	for source, want := range prefs.Sources {
		if got.For(source) != want {
			t.Errorf("%s preference: %+v, want %+v", source, got.For(source), want)
		}
	}
	for _, source := range []string{"unlisted", "audible", "silent", "in-app", "muted"} {
		want := prefs.For(source)
		result, err := s.Notify(ctx, source, "Sound preference test", "", Info)
		if err != nil || result.Stored != want.Enabled || result.Native != (want.Enabled && want.Native) || result.Sound != (want.Enabled && want.Sound) {
			t.Errorf("%s notification: %+v, %v", source, result, err)
		}
	}
	rows, err := s.List(ctx, ListOptions{})
	if err != nil || len(rows) != 4 {
		t.Fatalf("silent notifications must persist, disabled notifications must not: %+v, %v", rows, err)
	}
}

func TestLegacySoundPreferences(t *testing.T) {
	cases := []struct {
		name                                  string
		raw                                   string
		wantDefault, wantLegacy, wantExplicit bool
	}{
		{
			name:        "legacy defaults and source",
			raw:         `{"default":{"enabled":true,"native":true},"sources":{"legacy":{"enabled":true,"native":true},"explicit":{"enabled":true,"native":true,"sound":false}}}`,
			wantDefault: true, wantLegacy: true, wantExplicit: false,
		},
		{
			name:        "explicit silent default does not rewrite legacy source",
			raw:         `{"default":{"enabled":true,"native":true,"sound":false},"sources":{"legacy":{"enabled":true,"native":true},"explicit":{"enabled":true,"native":true,"sound":true}}}`,
			wantDefault: false, wantLegacy: true, wantExplicit: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(open(t, filepath.Join(t.TempDir(), "orole.db")).DB)
			if _, err := s.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,0)`, PrefsKey, tc.raw); err != nil {
				t.Fatal(err)
			}
			prefs, err := s.Prefs(ctx)
			if err != nil || prefs.Default.Sound != tc.wantDefault || prefs.For("legacy").Sound != tc.wantLegacy || prefs.For("explicit").Sound != tc.wantExplicit {
				t.Fatalf("decoded preferences: %+v, %v", prefs, err)
			}
			// Saving migrated preferences must preserve both defaulted true and explicit false.
			if err := s.SetPrefs(ctx, prefs); err != nil {
				t.Fatal(err)
			}
			for source, wantSound := range map[string]bool{"unlisted": tc.wantDefault, "legacy": tc.wantLegacy, "explicit": tc.wantExplicit} {
				result, err := s.Notify(ctx, source, "Legacy sound test", "", Info)
				if err != nil || !result.Stored || !result.Native || result.Sound != wantSound {
					t.Errorf("%s notification after save: %+v, %v", source, result, err)
				}
			}
		})
	}
}

func TestPrunesOldestReadFirst(t *testing.T) {
	s := New(open(t, filepath.Join(t.TempDir(), "orole.db")).DB)
	// Insert MaxKeep rows directly, the oldest unread, then add read ones.
	tx, _ := s.DB.Begin()
	for i := range MaxKeep {
		read := 0
		if i%2 == 1 {
			read = 1
		}
		if _, err := tx.Exec(`INSERT INTO notifications(id,at,source,title,read) VALUES(?,?,?,?,?)`, fmt.Sprintf("bulk-%d", i), int64(1000+i), "bulk", "t", read); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit()
	if _, err := s.Notify(ctx, "bulk", "newest", "", Info); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n)
	if n != MaxKeep {
		t.Fatalf("rows %d, want %d", n, MaxKeep)
	}
	var newest int
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE title='newest'`).Scan(&newest)
	if newest != 1 {
		t.Fatal("newest notification pruned")
	}
	var oldestRead int
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id LIKE 'bulk-%' AND at=1001`).Scan(&oldestRead)
	if oldestRead != 0 {
		t.Fatal("oldest read row should have been pruned first")
	}
}
