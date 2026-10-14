package jarvis

import (
	"testing"
	"time"
)

func TestMatches(t *testing.T) {
	now := int64(5_000)
	cases := []struct {
		name string
		r    Rule
		ev   Event
		want bool
	}{
		{"specific task", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFinished, TargetID: "t1"}, true},
		{"other task", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFinished, TargetID: "t2"}, false},
		{"any own task", Rule{BotID: "b1", Trigger: TaskFailed, Target: "any"}, Event{Trigger: TaskFailed, TargetID: "t9", OwnerBotID: "b1"}, true},
		{"any task of another bot", Rule{BotID: "b1", Trigger: TaskFailed, Target: "any"}, Event{Trigger: TaskFailed, TargetID: "t9", OwnerBotID: "b2"}, false},
		{"any automation", Rule{BotID: "b1", Trigger: AutomationFailed, Target: "any"}, Event{Trigger: AutomationFailed, TargetID: "a1"}, true},
		{"wrong trigger", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFailed, TargetID: "t1"}, false},
		{"notification kind", Rule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, true},
		{"notification kind+source", Rule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:github"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, true},
		{"notification wrong source", Rule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:mail"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, false},
		{"expired", Rule{BotID: "b1", Trigger: TaskFinished, Target: "t1", ExpiresAt: 4_000}, Event{Trigger: TaskFinished, TargetID: "t1"}, false},
		{"at_time never matches events", Rule{BotID: "b1", Trigger: AtTime, Target: "08:00"}, Event{Trigger: AtTime}, false},
	}
	for _, c := range cases {
		if got := Matches(c.r, c.ev, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestNextAt(t *testing.T) {
	loc := time.FixedZone("x", 2*3600)
	r := Rule{Trigger: AtTime, Target: "08:30"}
	before := time.Date(2026, 10, 14, 7, 0, 0, 0, loc)
	if got := NextAt(r, before); !got.Equal(time.Date(2026, 10, 14, 8, 30, 0, 0, loc)) {
		t.Errorf("same day: %v", got)
	}
	after := time.Date(2026, 10, 14, 9, 0, 0, 0, loc)
	if got := NextAt(r, after); !got.Equal(time.Date(2026, 10, 15, 8, 30, 0, 0, loc)) {
		t.Errorf("next day: %v", got)
	}
	if !NextAt(Rule{Trigger: TaskFinished, Target: "x"}, after).IsZero() {
		t.Error("non-time rule has a next time")
	}
}
