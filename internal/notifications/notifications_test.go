package notifications

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

func store(t *testing.T) *Store {
	return New(open(t, filepath.Join(t.TempDir(), "orole.db")).DB)
}

func TestNotifyPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orole.db")
	db := open(t, path)
	s := New(db.DB)
	r, err := s.Notify(ctx, Input{Source: "task", Kind: Finished, Title: "Fix login", Body: "details", Level: Success, Target: "task:t1"})
	if err != nil || !r.Stored || !r.Sound {
		t.Fatalf("notify: %+v %v", r, err)
	}
	if _, err := s.Notify(ctx, Input{Source: "automation/a1", Title: "Deploy note"}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s2 := New(open(t, path).DB)
	list, err := s2.List(ctx, ListOptions{})
	if err != nil || len(list) != 2 {
		t.Fatalf("after reopen: %+v %v", list, err)
	}
	if list[0].Kind != Message || list[0].Level != Info || list[1].Kind != Finished || list[1].Target != "task:t1" || list[1].Body != "details" || list[1].Read || list[1].NeedsYou {
		t.Fatalf("rows: %+v", list)
	}
	if c, _ := s2.Counts(ctx); c.Unread != 2 || c.NeedsYou != 0 {
		t.Fatalf("counts %+v", c)
	}
}

func TestApprovalsStayInNeedsYouUntilResolved(t *testing.T) {
	s := store(t)
	approval := Input{Source: "task", Kind: Approval, Title: "Fix login", Body: "Run tests?", Target: "task:t1", Ref: "task:t1"}
	if r, err := s.Notify(ctx, approval); err != nil || !r.Stored || !r.Notification.NeedsYou {
		t.Fatalf("approval: %+v %v", r, err)
	}
	// The same wait reported again (a restart, a repeated status) does not stack.
	if r, err := s.Notify(ctx, approval); err != nil || r.Stored {
		t.Fatalf("duplicate open approval stored: %+v %v", r, err)
	}
	other := approval
	other.Ref, other.Target = "run:r1", "automation:a1"
	if r, _ := s.Notify(ctx, other); !r.Stored {
		t.Fatal("approval for another wait was dropped")
	}
	if _, err := s.Notify(ctx, Input{Source: "bot", Kind: Reply, Title: "Ada", Body: "Hi"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.List(ctx, ListOptions{NeedsYou: true}); len(l) != 2 {
		t.Fatalf("needs-you list: %+v", l)
	}
	// Reading an approval does not end the wait it reports.
	if _, err := s.MarkRead(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Counts(ctx); c.Unread != 0 || c.NeedsYou != 2 {
		t.Fatalf("after read: %+v", c)
	}
	if n, err := s.Resolve(ctx, "task:t1"); err != nil || n != 1 {
		t.Fatalf("resolve: %d %v", n, err)
	}
	if l, _ := s.List(ctx, ListOptions{NeedsYou: true}); len(l) != 1 || l[0].Target != "automation:a1" {
		t.Fatalf("after resolve: %+v", l)
	}
	if refs, _ := s.OpenRefs(ctx); len(refs) != 1 || refs[0] != "run:r1" {
		t.Fatalf("open refs: %v", refs)
	}
	// A new wait after the old one ended is reported again; history keeps the resolved one.
	if r, _ := s.Notify(ctx, approval); !r.Stored {
		t.Fatal("new wait with a resolved ref was dropped")
	}
	if l, _ := s.List(ctx, ListOptions{}); len(l) != 4 {
		t.Fatalf("history: %d rows", len(l))
	}
}

func TestMarkRead(t *testing.T) {
	s := store(t)
	var ids []string
	for i := range 3 {
		r, err := s.Notify(ctx, Input{Source: "bot", Kind: Reply, Title: fmt.Sprint("reply ", i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Notification.ID)
	}
	if n, err := s.MarkRead(ctx, ids[:1], false); err != nil || n != 1 {
		t.Fatalf("mark one: %d %v", n, err)
	}
	if n, _ := s.MarkRead(ctx, ids[:1], false); n != 0 {
		t.Fatal("re-marking a read row reported a change")
	}
	if c, _ := s.Counts(ctx); c.Unread != 2 {
		t.Fatalf("unread %d", c.Unread)
	}
	if n, _ := s.MarkRead(ctx, nil, true); n != 2 {
		t.Fatalf("mark all: %d", n)
	}
	if _, err := s.MarkRead(ctx, nil, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty selection: %v", err)
	}
}

func TestValidation(t *testing.T) {
	s := store(t)
	bad := []Input{
		{Source: "", Title: "t"},
		{Source: "Bad Source", Title: "t"},
		{Source: "ok", Title: "   "},
		{Source: "ok", Title: string(make([]rune, MaxTitle+1))},
		{Source: "ok", Title: "t", Body: string(make([]rune, MaxBody+1))},
		{Source: "ok", Title: "t", Level: "loud"},
		{Source: "ok", Title: "t", Kind: "mention"},
		{Source: "ok", Title: "t", Kind: Approval}, // an approval nobody can resolve
	}
	for i, in := range bad {
		if _, err := s.Notify(ctx, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d: want ErrInvalid, got %v", i, err)
		}
	}
}

func TestPreferencesPerKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orole.db")
	db := open(t, path)
	s := New(db.DB)
	stored, err := s.SetPrefs(ctx, Prefs{Kinds: map[Kind]KindPref{
		Reply:    {InApp: false, Sound: true},
		Finished: {InApp: true, Sound: false},
		Approval: {InApp: false, Sound: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Approvals cannot be hidden: something would wait on the user with no sign of it.
	if got := stored.Kinds[Approval]; !got.InApp || got.Sound {
		t.Fatalf("approval pref: %+v", got)
	}
	if got := stored.Kinds[Failed]; !got.InApp || !got.Sound {
		t.Fatalf("unset kind must default on: %+v", got)
	}
	db.Close()

	s = New(open(t, path).DB)
	for _, tc := range []struct {
		in           Input
		stored, beep bool
	}{
		{Input{Source: "bot", Kind: Reply, Title: "Ada"}, false, false},
		{Input{Source: "task", Kind: Finished, Title: "Done"}, true, false},
		{Input{Source: "task", Kind: Approval, Title: "Wait", Ref: "task:1"}, true, false},
		{Input{Source: "task", Kind: Failed, Title: "Broke"}, true, true},
	} {
		r, err := s.Notify(ctx, tc.in)
		if err != nil || r.Stored != tc.stored || r.Sound != tc.beep {
			t.Errorf("%s: %+v %v", tc.in.Kind, r, err)
		}
	}
	if _, err := s.SetPrefs(ctx, Prefs{Kinds: map[Kind]KindPref{"mention": {}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown kind: %v", err)
	}
}

func TestOlderPreferenceShapeReadsAsDefaults(t *testing.T) {
	s := store(t)
	if _, err := s.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,0)`, PrefsKey, `{"default":{"enabled":false,"native":true,"sound":false},"sources":{"mail":{"enabled":false}}}`); err != nil {
		t.Fatal(err)
	}
	p, err := s.Prefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range Kinds {
		if got := p.For(k); !got.InApp || !got.Sound {
			t.Errorf("%s: %+v", k, got)
		}
	}
}

func TestPruneKeepsOpenApprovalsAndUnread(t *testing.T) {
	s := store(t)
	tx, _ := s.DB.Begin()
	for i := range MaxKeep {
		read, needsYou := 0, 0
		switch i % 3 {
		case 1:
			read = 1
		case 2:
			read, needsYou = 1, 1
		}
		if _, err := tx.Exec(`INSERT INTO notifications(id,at,source,title,read,kind,ref,needs_you) VALUES(?,?,?,?,?,?,?,?)`, fmt.Sprintf("bulk-%d", i), int64(1000+i), "bulk", "t", read, "approval", fmt.Sprintf("ref-%d", i), needsYou); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit()
	if _, err := s.Notify(ctx, Input{Source: "bulk", Title: "newest"}); err != nil {
		t.Fatal(err)
	}
	var n, newest, oldestRead, oldestOpen int
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n)
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE title='newest'`).Scan(&newest)
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id='bulk-1'`).Scan(&oldestRead)
	s.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id='bulk-2'`).Scan(&oldestOpen)
	if n != MaxKeep || newest != 1 || oldestRead != 0 || oldestOpen != 1 {
		t.Fatalf("rows=%d newest=%d oldestRead=%d oldestOpen=%d", n, newest, oldestRead, oldestOpen)
	}
}
