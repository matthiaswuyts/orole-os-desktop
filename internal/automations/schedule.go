package automations

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinInterval is the shortest period a schedule may have.
const MinInterval = time.Minute

// Schedule computes fire times. Implementations are immutable.
type Schedule interface {
	// Next returns the first fire time strictly after t. ok is false if none exists.
	Next(t time.Time) (next time.Time, ok bool)
	String() string
}

// ParseSchedule parses a 5-field cron expression ("min hour dom month dow"),
// an alias (@hourly, @daily, @weekly, @monthly, @yearly) or "every <n><m|h|d>".
// Cron times are evaluated in loc (the local time zone when nil).
func ParseSchedule(s string, loc *time.Location) (Schedule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("schedule is empty")
	}
	if loc == nil {
		loc = time.Local
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "every"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
		return parseEvery(strings.TrimSpace(rest))
	}
	switch strings.ToLower(s) {
	case "@hourly":
		s = "0 * * * *"
	case "@daily", "@midnight":
		s = "0 0 * * *"
	case "@weekly":
		s = "0 0 * * 0"
	case "@monthly":
		s = "0 0 1 * *"
	case "@yearly", "@annually":
		s = "0 0 1 1 *"
	}
	return parseCron(s, loc)
}

type every struct{ d time.Duration }

func parseEvery(arg string) (Schedule, error) {
	if len(arg) < 2 {
		return nil, fmt.Errorf("expected 'every <number><m|h|d>', e.g. 'every 15m'")
	}
	n, err := strconv.Atoi(strings.TrimSpace(arg[:len(arg)-1]))
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("interval %q: expected a positive whole number followed by m, h or d", arg)
	}
	var unit time.Duration
	switch arg[len(arg)-1] {
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	default:
		return nil, fmt.Errorf("interval %q: unit must be m, h or d", arg)
	}
	if n > 366*24*60 {
		return nil, fmt.Errorf("interval %q is too large", arg)
	}
	d := time.Duration(n) * unit
	if d < MinInterval || d > 366*24*time.Hour {
		return nil, fmt.Errorf("interval must be between %s and 366d", MinInterval)
	}
	return every{d}, nil
}

func (e every) Next(t time.Time) (time.Time, bool) { return t.Add(e.d), true }
func (e every) String() string {
	switch {
	case e.d%(24*time.Hour) == 0:
		return fmt.Sprintf("every %dd", e.d/(24*time.Hour))
	case e.d%time.Hour == 0:
		return fmt.Sprintf("every %dh", e.d/time.Hour)
	}
	return fmt.Sprintf("every %dm", e.d/time.Minute)
}

// cron is a parsed 5-field expression. Each field is a bitset of allowed values.
type cron struct {
	src                 string
	min, hour, dom, mon uint64
	dow                 uint64 // bit 0 = Sunday
	domStar, dowStar    bool
	loc                 *time.Location
}

var (
	monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
	dowNames   = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
)

func parseCron(s string, loc *time.Location) (Schedule, error) {
	f := strings.Fields(s)
	if len(f) != 5 {
		return nil, fmt.Errorf("expected 5 fields (minute hour day-of-month month day-of-week) or 'every 15m', got %d", len(f))
	}
	c := &cron{src: strings.Join(f, " "), loc: loc}
	var err error
	if c.min, _, err = parseField(f[0], 0, 59, nil, "minute"); err != nil {
		return nil, err
	}
	if c.hour, _, err = parseField(f[1], 0, 23, nil, "hour"); err != nil {
		return nil, err
	}
	if c.dom, c.domStar, err = parseField(f[2], 1, 31, nil, "day-of-month"); err != nil {
		return nil, err
	}
	if c.mon, _, err = parseField(f[3], 1, 12, monthNames, "month"); err != nil {
		return nil, err
	}
	if c.dow, c.dowStar, err = parseField(f[4], 0, 7, dowNames, "day-of-week"); err != nil {
		return nil, err
	}
	if c.dow&(1<<7) != 0 { // 7 is also Sunday
		c.dow = c.dow&^(1<<7) | 1
	}
	return c, nil
}

// parseField parses "*", "n", "a-b", "*/s", "a-b/s" and comma lists.
// star reports a field that starts with "*" (unrestricted for day matching).
func parseField(s string, lo, hi int, names map[string]int, what string) (mask uint64, star bool, err error) {
	val := func(x string) (int, error) {
		if n, ok := names[strings.ToLower(x)]; ok {
			return n, nil
		}
		n, err := strconv.Atoi(x)
		if err != nil || n < lo || n > hi || x[0] == '+' || x[0] == '-' {
			return 0, fmt.Errorf("%s: %q is not valid (allowed %d-%d)", what, x, lo, hi)
		}
		return n, nil
	}
	if s == "" {
		return 0, false, fmt.Errorf("%s: empty field", what)
	}
	for _, part := range strings.Split(s, ",") {
		rng, stepStr, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 || stepStr[0] == '+' {
				return 0, false, fmt.Errorf("%s: bad step %q", what, stepStr)
			}
			step = n
		}
		var a, b int
		switch {
		case rng == "*":
			a, b = lo, hi
			if what == "day-of-week" {
				b = 6
			}
			if part == s || strings.HasPrefix(s, "*") {
				star = true
			}
		case strings.Contains(rng, "-"):
			x, y, _ := strings.Cut(rng, "-")
			if a, err = val(x); err != nil {
				return 0, false, err
			}
			if b, err = val(y); err != nil {
				return 0, false, err
			}
			if a > b {
				return 0, false, fmt.Errorf("%s: range %q runs backwards", what, rng)
			}
		default:
			if a, err = val(rng); err != nil {
				return 0, false, err
			}
			b = a
			if hasStep { // "5/10" means 5-max/10
				b = hi
			}
		}
		for v := a; v <= b; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, star, nil
}

func (c *cron) String() string { return c.src }

func has(mask uint64, v int) bool { return mask&(1<<uint(v)) != 0 }

func (c *cron) dayOK(t time.Time) bool {
	domOK := has(c.dom, t.Day())
	dowOK := has(c.dow, int(t.Weekday()))
	if c.domStar || c.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK // classic cron: both restricted means either
}

// Next implements Schedule. It walks wall-clock fields in c.loc, so it follows
// DST: a time that does not exist on a given day is skipped for that day, and a
// repeated hour fires once.
func (c *cron) Next(after time.Time) (time.Time, bool) {
	t := after.In(c.loc).Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(5, 0, 0)
	for iter := 0; iter < 200000 && t.Before(limit); iter++ {
		y, mo, d := t.Date()
		switch {
		case !has(c.mon, int(mo)):
			t = bumpTo(t, time.Date(y, mo+1, 1, 0, 0, 0, 0, c.loc), time.Hour*24)
		case !c.dayOK(t):
			t = bumpTo(t, time.Date(y, mo, d+1, 0, 0, 0, 0, c.loc), time.Hour)
		case !has(c.hour, t.Hour()):
			t = bumpTo(t, time.Date(y, mo, d, t.Hour()+1, 0, 0, 0, c.loc), time.Minute)
		case !has(c.min, t.Minute()):
			t = t.Add(time.Minute)
		default:
			if t.After(after) && !repeatedWallTime(t) {
				return t, true
			}
			t = t.Add(time.Minute)
		}
	}
	return time.Time{}, false
}

// repeatedWallTime reports whether t is the second occurrence of a wall-clock
// time that happens twice (the hour that repeats when clocks go back). Cron
// fires such times once, on the first pass.
func repeatedWallTime(t time.Time) bool {
	start, _ := t.ZoneBounds()
	if start.IsZero() {
		return false
	}
	_, previousOffset := start.Add(-time.Second).Zone()
	_, offset := t.Zone()
	repeated := time.Duration(previousOffset-offset) * time.Second
	return repeated > 0 && t.Before(start.Add(repeated))
}

// bumpTo returns target, or cur+fallback when normalisation of a DST-ambiguous
// wall time would not move forward.
func bumpTo(cur, target time.Time, fallback time.Duration) time.Time {
	if target.After(cur) {
		return target
	}
	return cur.Add(fallback)
}

// NextRuns lists up to n upcoming fire times after t.
func NextRuns(s Schedule, t time.Time, n int) []time.Time {
	var out []time.Time
	for range n {
		next, ok := s.Next(t)
		if !ok {
			break
		}
		out = append(out, next)
		t = next
	}
	return out
}
