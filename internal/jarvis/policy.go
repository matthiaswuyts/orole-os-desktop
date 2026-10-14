package jarvis

import (
	"fmt"
	"time"
)

const coolDown = 10 * time.Minute

type Usage struct {
	Today           int   // calls placed since local midnight
	MonthCostMicros int64 // estimated cost since the 1st of the month (1 USD = 1_000_000)
	LastBotCallMs   int64 // last outbound call of this bot
	InCall          bool
}

type Ask struct {
	Judgement     bool // call_owner (not a rule): the cool-down applies
	QuietOverride bool
}

type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

func Allow(s Settings, now time.Time, u Usage, a Ask) error {
	if u.InCall {
		return &Refusal{"A call is already in progress."}
	}
	if !a.QuietOverride && InQuietHours(s, now) {
		return &Refusal{"It is quiet hours."}
	}
	if u.Today >= s.dailyCap() {
		return &Refusal{fmt.Sprintf("The daily limit of %d calls is reached.", s.dailyCap())}
	}
	if u.MonthCostMicros >= int64(s.monthlyCapCents())*10_000 {
		return &Refusal{fmt.Sprintf("The monthly cost limit of $%.2f is reached.", float64(s.monthlyCapCents())/100)}
	}
	if a.Judgement && u.LastBotCallMs > 0 && now.Sub(time.UnixMilli(u.LastBotCallMs)) < coolDown {
		return &Refusal{"This bot called less than 10 minutes ago."}
	}
	return nil
}

func minutes(hhmmStr string) (int, bool) {
	if !hhmm.MatchString(hhmmStr) {
		return 0, false
	}
	return int(hhmmStr[0]-'0')*600 + int(hhmmStr[1]-'0')*60 + int(hhmmStr[3]-'0')*10 + int(hhmmStr[4]-'0'), true
}

// InQuietHours reports whether now (in its own location) falls in [QuietStart, QuietEnd), wrapping midnight.
func InQuietHours(s Settings, now time.Time) bool {
	start, ok1 := minutes(s.QuietStart)
	end, ok2 := minutes(s.QuietEnd)
	if !ok1 || !ok2 || start == end {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}
