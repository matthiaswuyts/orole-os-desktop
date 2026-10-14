package jarvis

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func testStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO bots(id,name,created_at,updated_at) VALUES('b1','Atlas',0,0),('b2','Nova',0,0)`); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_000_000)
	return NewStore(db.DB, func() time.Time { return now }), &now
}

func TestRulesRoundTrip(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	r, err := s.AddRule(ctx, Rule{BotID: "b1", Trigger: TaskFinished, Target: "any", Note: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.CreatedAt != 1_000_000 {
		t.Fatalf("rule not stamped: %+v", r)
	}
	if _, err := s.AddRule(ctx, Rule{BotID: "b2", Trigger: AtTime, Target: "08:00", Repeat: true}); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.Rules(ctx, "b1")
	all, _ := s.Rules(ctx, "")
	if len(mine) != 1 || len(all) != 2 || mine[0].Note != "deploy" {
		t.Fatalf("mine=%v all=%v", mine, all)
	}
	if err := s.DeleteRule(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRule(ctx, r.ID); err != ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}
}

func TestAddRuleRejectsBadInput(t *testing.T) {
	s, _ := testStore(t)
	for _, r := range []Rule{
		{BotID: "b1", Trigger: "bogus"},
		{BotID: "b1", Trigger: AtTime, Target: "25:00"},
		{BotID: "b1", Trigger: TaskFailed, Target: ""},
		{BotID: "b1", Trigger: TaskFailed, Target: "any", Note: string(make([]byte, 281))},
	} {
		if _, err := s.AddRule(context.Background(), r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
}

func TestCallsSinceSumsCost(t *testing.T) {
	s, now := testStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		c := Call{BotID: "b1", Direction: Outbound, Status: StatusCompleted, EstCostMicros: 100_000}
		if err := s.InsertCall(ctx, &c); err != nil {
			t.Fatal(err)
		}
	}
	n, cost, err := s.CallsSince(ctx, now.UnixMilli()-1)
	if err != nil || n != 3 || cost != 300_000 {
		t.Fatalf("n=%d cost=%d err=%v", n, cost, err)
	}
	last, _ := s.LastCallAt(ctx, "b1")
	if last != now.UnixMilli() {
		t.Fatalf("last=%d", last)
	}
}
