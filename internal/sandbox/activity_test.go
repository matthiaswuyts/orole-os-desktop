package sandbox

import (
	"fmt"
	"testing"
)

func TestActivityRecordsRunningThenFinished(t *testing.T) {
	var seen []ActivityEntry
	a := NewActivity(func(e ActivityEntry) { seen = append(seen, e) })
	id := a.Begin("b1", ActivityCommand, "ls")
	a.End("b1", id, ActivityDone, "file.txt\n", 0, false)
	if len(seen) != 2 || seen[0].Status != ActivityRunning || seen[1].Status != ActivityDone || seen[1].Output != "file.txt\n" {
		t.Fatalf("events = %+v", seen)
	}
	if got := a.List("b1"); len(got) != 1 || got[0].ID != id || got[0].Status != ActivityDone {
		t.Fatalf("list = %+v", got)
	}
	if got := a.List("other"); len(got) != 0 {
		t.Fatalf("other bot's list = %+v", got)
	}
}

func TestActivityKeepsTheNewestEntries(t *testing.T) {
	a := NewActivity(nil)
	for i := 0; i < activityMax+5; i++ {
		a.Begin("b1", ActivityCommand, fmt.Sprint("cmd", i))
	}
	got := a.List("b1")
	if len(got) != activityMax || got[0].Title != "cmd5" || got[len(got)-1].Title != fmt.Sprint("cmd", activityMax+4) {
		t.Fatalf("kept %d, first %q, last %q", len(got), got[0].Title, got[len(got)-1].Title)
	}
}

func TestNilActivityIsInert(t *testing.T) {
	var a *Activity
	id := a.Begin("b1", ActivityCommand, "ls")
	a.End("b1", id, ActivityDone, "", 0, false)
	if len(a.List("b1")) != 0 {
		t.Fatal("a nil Activity listed entries")
	}
}
