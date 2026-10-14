package jarvis

import (
	"strings"
	"time"
)

// Event is something a rule can fire on. The daemon builds it from task, automation and notification hooks.
type Event struct {
	Trigger    JarvisTrigger
	OwnerBotID string // bot that started the task, or the bot a report came back to
	TargetID   string // task id, automation id, or notification kind
	Source     string // notification source (e.g. "github.ci"), or the name of the bot that reported back
	Title      string // short human title for the brief
	Detail     string // untrusted detail (task result, error); fenced before it reaches a model
}

func Expired(r JarvisRule, nowMs int64) bool { return r.ExpiresAt > 0 && nowMs >= r.ExpiresAt }

func Matches(r JarvisRule, ev Event, nowMs int64) bool {
	if r.Trigger == AtTime || r.Trigger != ev.Trigger || Expired(r, nowMs) {
		return false
	}
	switch r.Trigger {
	case TaskFinished, TaskFailed:
		if r.Target == "any" {
			return ev.OwnerBotID == r.BotID
		}
		return r.Target == ev.TargetID
	case AutomationFinished, AutomationFailed:
		return r.Target == "any" || r.Target == ev.TargetID
	case BotReported:
		if r.BotID != ev.OwnerBotID {
			return false
		}
		from := strings.TrimSpace(ev.Source)
		return r.Target == "any" || from != "" && strings.EqualFold(strings.TrimSpace(r.Target), from)
	case NotificationTrigger:
		kind, source, _ := strings.Cut(r.Target, ":")
		if kind != ev.TargetID {
			return false
		}
		return source == "" || sourceMatches(ev.Source, source)
	}
	return false
}

// NextAt is the next wall-clock occurrence of an at_time rule strictly after `after`, in after's location.
func NextAt(r JarvisRule, after time.Time) time.Time {
	if r.Trigger != AtTime {
		return time.Time{}
	}
	m, ok := minutes(r.Target)
	if !ok {
		return time.Time{}
	}
	y, mo, d := after.Date()
	t := time.Date(y, mo, d, m/60, m%60, 0, 0, after.Location())
	if !t.After(after) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// sourceMatches reports whether a notification source is want or nested under it: "github" matches "github",
// "github.ci" and "github/ci", never "githubber".
func sourceMatches(source, want string) bool {
	rest, ok := strings.CutPrefix(source, want)
	return ok && (rest == "" || rest[0] == '.' || rest[0] == '/')
}
