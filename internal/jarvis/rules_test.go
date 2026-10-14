package jarvis

import (
	"testing"
	"time"
)

func TestMatches(t *testing.T) {
	now := int64(5_000)
	cases := []struct {
		name string
		r    JarvisRule
		ev   Event
		want bool
	}{
		{"specific task", JarvisRule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFinished, TargetID: "t1"}, true},
		{"other task", JarvisRule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFinished, TargetID: "t2"}, false},
		{"any own task", JarvisRule{BotID: "b1", Trigger: TaskFailed, Target: "any"}, Event{Trigger: TaskFailed, TargetID: "t9", OwnerBotID: "b1"}, true},
		{"any task of another bot", JarvisRule{BotID: "b1", Trigger: TaskFailed, Target: "any"}, Event{Trigger: TaskFailed, TargetID: "t9", OwnerBotID: "b2"}, false},
		{"any automation", JarvisRule{BotID: "b1", Trigger: AutomationFailed, Target: "any"}, Event{Trigger: AutomationFailed, TargetID: "a1"}, true},
		{"wrong trigger", JarvisRule{BotID: "b1", Trigger: TaskFinished, Target: "t1"}, Event{Trigger: TaskFailed, TargetID: "t1"}, false},
		{"notification kind", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, true},
		{"notification kind+source", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:github"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, true},
		{"notification wrong source", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:mail"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, false},
		{"notification exact source", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:github"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github"}, true},
		{"notification nested source", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:github"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github/ci"}, true},
		{"notification prefix is not a source", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:git"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "github.ci"}, false},
		{"notification lookalike source", JarvisRule{BotID: "b1", Trigger: NotificationTrigger, Target: "failed:github"}, Event{Trigger: NotificationTrigger, TargetID: "failed", Source: "githubber"}, false},
		{"expired", JarvisRule{BotID: "b1", Trigger: TaskFinished, Target: "t1", ExpiresAt: 4_000}, Event{Trigger: TaskFinished, TargetID: "t1"}, false},
		{"at_time never matches events", JarvisRule{BotID: "b1", Trigger: AtTime, Target: "08:00"}, Event{Trigger: AtTime}, false},
	}
	for _, c := range cases {
		if got := Matches(c.r, c.ev, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestNextAt(t *testing.T) {
	loc := time.FixedZone("x", 2*3600)
	r := JarvisRule{Trigger: AtTime, Target: "08:30"}
	before := time.Date(2026, 10, 14, 7, 0, 0, 0, loc)
	if got := NextAt(r, before); !got.Equal(time.Date(2026, 10, 14, 8, 30, 0, 0, loc)) {
		t.Errorf("same day: %v", got)
	}
	after := time.Date(2026, 10, 14, 9, 0, 0, 0, loc)
	if got := NextAt(r, after); !got.Equal(time.Date(2026, 10, 15, 8, 30, 0, 0, loc)) {
		t.Errorf("next day: %v", got)
	}
	if !NextAt(JarvisRule{Trigger: TaskFinished, Target: "x"}, after).IsZero() {
		t.Error("non-time rule has a next time")
	}
}
