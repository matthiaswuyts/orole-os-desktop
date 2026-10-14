package jarvis

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCheckGate(t *testing.T) {
	full := GateInput{ToolAccess: "all", TaskMode: "yolo", BotYolo: true, CodeTasks: true}
	if err := CheckGate(full); err != nil {
		t.Fatalf("full: %v", err)
	}
	cases := map[string]func(*GateInput){
		"tool access": func(g *GateInput) { g.ToolAccess = "write" },
		"task mode":   func(g *GateInput) { g.TaskMode = "auto" },
		"bot yolo":    func(g *GateInput) { g.BotYolo = false },
		"yolo off":    func(g *GateInput) { g.YoloDisabled = true },
		"code tasks":  func(g *GateInput) { g.CodeTasks = false },
	}
	for name, mod := range cases {
		g := full
		mod(&g)
		err := CheckGate(g)
		var ge *GateError
		if err == nil || !errorsAs(err, &ge) || ge.Reason == "" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAllowPolicy(t *testing.T) {
	loc := time.FixedZone("x", 0)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 14, h, m, 0, 0, loc) }
	s := PhoneSettings{QuietStart: "22:00", QuietEnd: "07:00"}
	if err := Allow(s, at(12, 0), Usage{}, Ask{}); err != nil {
		t.Fatalf("noon: %v", err)
	}
	if Allow(s, at(23, 0), Usage{}, Ask{}) == nil {
		t.Error("quiet hours not enforced at 23:00")
	}
	if Allow(s, at(6, 59), Usage{}, Ask{}) == nil {
		t.Error("quiet hours not enforced at 06:59 (wraps midnight)")
	}
	if err := Allow(s, at(23, 0), Usage{}, Ask{QuietOverride: true}); err != nil {
		t.Errorf("override: %v", err)
	}
	if Allow(s, at(12, 0), Usage{Today: 10}, Ask{}) == nil {
		t.Error("daily cap 10 not enforced")
	}
	if Allow(s, at(12, 0), Usage{MonthCostMicros: 20_000_000}, Ask{}) == nil {
		t.Error("monthly cap $20 not enforced")
	}
	last := at(11, 55).UnixMilli()
	if Allow(s, at(12, 0), Usage{LastBotCallMs: last}, Ask{Judgement: true}) == nil {
		t.Error("cool-down not enforced for judgement calls")
	}
	if err := Allow(s, at(12, 0), Usage{LastBotCallMs: last}, Ask{}); err != nil {
		t.Errorf("rules skip cool-down: %v", err)
	}
	if Allow(s, at(12, 0), Usage{InCall: true}, Ask{}) == nil {
		t.Error("call in progress not refused")
	}
}

func TestCheckOpener(t *testing.T) {
	good := "Hey, Atlas here. The migration task failed on the auth tests."
	if got, ok := CheckOpener("Atlas", good); !ok || got != good {
		t.Fatalf("good opener rejected: %q", got)
	}
	for _, bad := range []string{
		"Hi, it's your assistant.",                // no name
		"Atlas from Orole here, task failed.",     // app name
		"Atlas here, " + strings.Repeat("x", 240), // too long
		"Atlas here.\n\nSecond paragraph.",        // multi paragraph
		"Atlasian bots say hi.",                   // name not on a word boundary
	} {
		if _, ok := CheckOpener("Atlas", bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	for i := 0; i < 6; i++ {
		f := FallbackOpener("Atlas", "the deploy finished", i)
		if _, ok := CheckOpener("Atlas", f); !ok {
			t.Errorf("fallback %d fails its own check: %q", i, f)
		}
	}
}

func TestCheckOpenerRejectsEmptyName(t *testing.T) {
	for _, name := range []string{"", "   ", "\t"} {
		if _, ok := CheckOpener(name, "Hey, anything here."); ok {
			t.Errorf("accepted with empty name %q", name)
		}
	}
}

func TestFallbackNonASCIIReason(t *testing.T) {
	reason := strings.Repeat("échec du déploiement ünd 日本語 ", 10)
	for i := 0; i < 6; i++ {
		f := FallbackOpener("Atlas", reason, i)
		if !utf8.ValidString(f) {
			t.Errorf("fallback %d is invalid UTF-8: %q", i, f)
		}
		if _, ok := CheckOpener("Atlas", f); !ok {
			t.Errorf("fallback %d fails CheckOpener: %q", i, f)
		}
	}
}

func TestFallbackAlwaysPassesCheck(t *testing.T) {
	names := []string{
		"Atlas", "日本", "Atlas!", "Nova  Bot", "Orolebot",
		strings.Repeat("A", 80), strings.Repeat("日", 80),
	}
	reasons := []string{
		"the deploy finished",
		strings.Repeat("échec du déploiement ünd 日本語 ", 20),
		"the deploy\nfailed\r\n on tests",
		"Orole sync failed",
	}
	for _, name := range names {
		for _, reason := range reasons {
			for seed := 0; seed < 6; seed++ {
				f := FallbackOpener(name, reason, seed)
				if !utf8.ValidString(f) {
					t.Errorf("name %q seed %d: invalid UTF-8", name, seed)
				}
				if n := utf8.RuneCountInString(f); n > maxOpener {
					t.Errorf("name %q seed %d: %d runes", name, seed, n)
				}
				if strings.Contains(f, "\n") {
					t.Errorf("name %q seed %d: newline in %q", name, seed, f)
				}
				if _, ok := CheckOpener(name, f); !ok {
					t.Errorf("name %q seed %d fails CheckOpener: %q", name, seed, f)
				}
			}
		}
	}
}

func TestCheckOpenerBoundaries(t *testing.T) {
	cases := []struct {
		name, text string
		want       bool
	}{
		{"Atlas", "Hey, Atlas here.", true},
		{"Atlas!", "Hey, Atlas! here.", true},
		{"Nova  Bot", "Hey, Nova Bot here.", true},
		{"Orolebot", "Hey, Orolebot here.", true},
		{"Atlas", "Atlas from Orole here.", false},
		{"Atlas", "Atlasian bots say hi.", false},
		{"Atlas", "Atlas1 here.", false},
		{"日本", "日本 here.", true},
		{"日本", "今日本 here.", false},
		{"Atlas", "Hi, it's your assistant.", false},
		{"Atlas", "Hey Atlas here.\nSecond.", false},
		{"", "Hey, anything here.", false},
		{"   ", "Hey, anything here.", false},
	}
	for _, c := range cases {
		if _, ok := CheckOpener(c.name, c.text); ok != c.want {
			t.Errorf("CheckOpener(%q, %q) = %v, want %v", c.name, c.text, ok, c.want)
		}
	}
}

func errorsAs(err error, target any) bool { return errors.As(err, target) }
