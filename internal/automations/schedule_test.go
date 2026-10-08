package automations

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata for %s unavailable: %v", name, err)
	}
	return l
}

func TestParseScheduleErrors(t *testing.T) {
	for _, s := range []string{
		"", "   ", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * 32 * *", "* * * 13 *", "* * * * 8",
		"*/0 * * * *", "5-1 * * * *", "a * * * *", "+5 * * * *", "*/-1 * * * *", "1,,2 * * * *", ", * * * *",
		"every", "every 0m", "every -5m", "every 5", "every 5s", "every 30s", "every m", "every 15x", "every 1.5h", "every 9999999d", "every 367d",
		"@never", "every15m",
	} {
		if sc, err := ParseSchedule(s, time.UTC); err == nil {
			t.Errorf("ParseSchedule(%q) = %v, want error", s, sc)
		}
	}
}

func ts(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCronNext(t *testing.T) {
	cases := []struct{ expr, after, want string }{
		{"* * * * *", "2026-03-01 10:00", "2026-03-01 10:01"},
		{"*/15 * * * *", "2026-03-01 10:00", "2026-03-01 10:15"},
		{"*/15 * * * *", "2026-03-01 10:50", "2026-03-01 11:00"},
		{"0 8 * * 1-5", "2026-03-06 08:00", "2026-03-09 08:00"}, // Fri -> Mon
		{"0 8 * * mon-fri", "2026-03-07 12:00", "2026-03-09 08:00"},
		{"30 2 29 2 *", "2026-03-01 00:00", "2028-02-29 02:30"},
		{"0 0 1 jan *", "2026-03-01 00:00", "2027-01-01 00:00"},
		{"5,10,15 9 * * *", "2026-03-01 09:10", "2026-03-01 09:15"},
		{"0 0 * * 7", "2026-03-01 00:00", "2026-03-08 00:00"}, // 7 == Sunday; 2026-03-01 is a Sunday
		{"0 0 * * 0", "2026-03-02 00:00", "2026-03-08 00:00"},
		{"0 12 13 * 5", "2026-03-01 00:00", "2026-03-06 12:00"}, // dom OR dow: first Friday (Mar 6) before the 13th
		{"0 12 13 * 5", "2026-03-06 12:00", "2026-03-13 12:00"}, // Fri 13th satisfies both
		{"0 12 13 * 5", "2026-03-13 12:00", "2026-03-20 12:00"}, // next Friday
		{"10-20/5 * * * *", "2026-03-01 00:00", "2026-03-01 00:10"},
		{"10-20/5 * * * *", "2026-03-01 00:10", "2026-03-01 00:15"},
		{"5/20 * * * *", "2026-03-01 00:06", "2026-03-01 00:25"},
		{"@hourly", "2026-03-01 10:30", "2026-03-01 11:00"},
		{"@daily", "2026-03-01 10:30", "2026-03-02 00:00"},
		{"@weekly", "2026-03-01 10:30", "2026-03-08 00:00"},
		{"@monthly", "2026-03-01 10:30", "2026-04-01 00:00"},
		{"@yearly", "2026-03-01 10:30", "2027-01-01 00:00"},
		{"  0   9   *   *   *  ", "2026-03-01 09:00", "2026-03-02 09:00"},
		{"every 15m", "2026-03-01 10:00", "2026-03-01 10:15"},
		{"EVERY 2h", "2026-03-01 10:00", "2026-03-01 12:00"},
		{"every 1d", "2026-03-01 10:00", "2026-03-02 10:00"},
	}
	for _, c := range cases {
		s, err := ParseSchedule(c.expr, time.UTC)
		if err != nil {
			t.Errorf("ParseSchedule(%q): %v", c.expr, err)
			continue
		}
		got, ok := s.Next(ts(c.after))
		if !ok || !got.Equal(ts(c.want)) {
			t.Errorf("%q after %s: got %v ok=%v, want %s", c.expr, c.after, got, ok, c.want)
		}
	}
}

func TestCronImpossibleDate(t *testing.T) {
	s, err := ParseSchedule("0 0 31 2 *", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if next, ok := s.Next(ts("2026-01-01 00:00")); ok {
		t.Fatalf("Feb 31 should never fire, got %v", next)
	}
}

func TestNextRuns(t *testing.T) {
	s, _ := ParseSchedule("0 */6 * * *", time.UTC)
	runs := NextRuns(s, ts("2026-03-01 05:00"), 5)
	want := []string{"2026-03-01 06:00", "2026-03-01 12:00", "2026-03-01 18:00", "2026-03-02 00:00", "2026-03-02 06:00"}
	if len(runs) != 5 {
		t.Fatalf("got %d runs", len(runs))
	}
	for i, w := range want {
		if !runs[i].Equal(ts(w)) {
			t.Errorf("run %d = %v want %s", i, runs[i], w)
		}
	}
}

func TestCronLocalTimezoneAndDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	s, _ := ParseSchedule("30 2 * * *", ny)
	// 2026-03-08 is the spring-forward day: 02:30 does not exist in New York.
	got, ok := s.Next(time.Date(2026, 3, 7, 12, 0, 0, 0, ny))
	if !ok {
		t.Fatal("no next")
	}
	if got.In(ny).Day() == 8 && got.In(ny).Hour() == 2 {
		t.Fatalf("fired at nonexistent wall time %v", got)
	}
	if !got.After(time.Date(2026, 3, 7, 12, 0, 0, 0, ny)) {
		t.Fatalf("not after: %v", got)
	}

	// Daily 09:00 keeps 09:00 local across the DST change (UTC offset moves).
	s, _ = ParseSchedule("0 9 * * *", ny)
	a, _ := s.Next(time.Date(2026, 3, 6, 10, 0, 0, 0, ny)) // Mar 7 09:00 EST = 14:00 UTC
	b, _ := s.Next(a)                                      // Mar 8 09:00 EDT = 13:00 UTC
	if a.In(ny).Hour() != 9 || b.In(ny).Hour() != 9 {
		t.Fatalf("local hour drifted: %v %v", a.In(ny), b.In(ny))
	}
	if a.UTC().Hour() != 14 || b.UTC().Hour() != 13 {
		t.Fatalf("expected UTC hour to follow the DST change: %v %v", a.UTC(), b.UTC())
	}

	// Fall back (2026-11-01): 01:30 happens twice but must fire exactly once that day.
	s, _ = ParseSchedule("30 1 * * *", ny)
	start := time.Date(2026, 10, 31, 12, 0, 0, 0, ny)
	var fires []time.Time
	cur := start
	for range 4 {
		n, ok := s.Next(cur)
		if !ok {
			t.Fatal("no next")
		}
		fires = append(fires, n)
		cur = n
	}
	seen := map[string]int{}
	for _, f := range fires {
		seen[f.In(ny).Format("2006-01-02")]++
	}
	for day, n := range seen {
		if n > 1 {
			t.Errorf("fired %d times on %s: %v", n, day, fires)
		}
	}
	for i := 1; i < len(fires); i++ {
		if !fires[i].After(fires[i-1]) {
			t.Errorf("non-increasing fires: %v", fires)
		}
	}
}
