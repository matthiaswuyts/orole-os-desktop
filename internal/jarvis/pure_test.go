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
	s := Settings{QuietStart: "22:00", QuietEnd: "07:00"}
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

func TestFallbackNewlineAndLongName(t *testing.T) {
	long := strings.Repeat("B", 300)
	for i := 0; i < 6; i++ {
		f := FallbackOpener("Atlas", "the deploy\nfailed\r\n on tests", i)
		if _, ok := CheckOpener("Atlas", f); !ok {
			t.Errorf("newline reason, fallback %d fails: %q", i, f)
		}
		g := FallbackOpener(long, strings.Repeat("x ", 200), i)
		if len(g) > maxOpener {
			t.Errorf("fallback %d over %d bytes: %d", i, maxOpener, len(g))
		}
		if _, ok := CheckOpener(long[:maxName], g); !ok {
			t.Errorf("long name, fallback %d fails: %q", i, g)
		}
	}
}

func errorsAs(err error, target any) bool { return errors.As(err, target) }
