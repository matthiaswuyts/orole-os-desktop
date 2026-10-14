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
		f := FallbackOpener("Atlas", i)
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

// The opener may reach voicemail, so it is a greeting with the bot's name and nothing about why it calls.
func TestFallbackIsNameOnlyGreeting(t *testing.T) {
	for seed := -1; seed < 8; seed++ {
		f := FallbackOpener("Atlas", seed)
		if !strings.Contains(f, "Atlas") || strings.Contains(strings.ToLower(f), "orole") {
			t.Errorf("seed %d: %q", seed, f)
		}
	}
	if openerInstruction("Atlas") == "" || strings.Contains(openerInstruction("Atlas"), "Context") {
		t.Fatal("opener instruction carries context")
	}
}

func TestFallbackAlwaysPassesCheck(t *testing.T) {
	names := []string{
		"Atlas", "日本", "Atlas!", "Nova  Bot", "Orolebot",
		strings.Repeat("A", 80), strings.Repeat("日", 80),
	}
	for _, name := range names {
		for seed := 0; seed < 6; seed++ {
			f := FallbackOpener(name, seed)
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

func TestIsVoicemail(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Hi, you've reached Ray. Please leave a message after the tone.", true},
		{"The person you are calling is not available.", true},
		{"Sorry, I can’t take your call right now", true},
		{"Welcome to the voicemail of Ray", true},
		{"This mailbox is full", true},
		{"Hallo, spreek een bericht in na de piep.", true},
		{"De abonnee is momenteel niet bereikbaar", true},
		{"Bonjour, laissez un message après le bip.", true},
		{"Vous êtes sur la messagerie de Ray", true},
		{"Bitte hinterlassen Sie eine Nachricht nach dem Signalton.", true},
		{"Die Mailbox von Ray", true},
		{"Hello?", false},
		{"Yes, Ray speaking", false},
		{"What's up Atlas?", false},
		{"Hallo, met Ray", false},
	}
	for _, c := range cases {
		if got := IsVoicemail(c.text); got != c.want {
			t.Errorf("IsVoicemail(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestBareGreeting(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Hello?", true}, {"hallo", true}, {"Yes?", true}, {"Hi", true}, {"Who is this?", true}, {"Met Ray", true},
		{"Hey, what's up?", true}, {"Yes, Ray speaking.", true},
		{"Hello, can you start the deploy for me?", false},
		{"What's running?", false},
		{"Start the nightly build now", false},
		{"", false},
	}
	for _, c := range cases {
		if got := bareGreeting(c.text); got != c.want {
			t.Errorf("bareGreeting(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestCarrierEnd(t *testing.T) {
	cases := []struct {
		status   string
		answered bool
		want     string
		ok       bool
	}{
		{"in-progress", true, "", false},
		{"ringing", false, "", false},
		{"queued", false, "", false},
		{"completed", true, StatusCompleted, true},
		{"completed", false, StatusNoAnswer, true},
		{"busy", false, StatusNoAnswer, true},
		{"no-answer", false, StatusNoAnswer, true},
		{"canceled", false, StatusNoAnswer, true},
		{"canceled", true, StatusCompleted, true},
		{"failed", false, StatusFailed, true},
	}
	for _, c := range cases {
		got, ok := carrierEnd(c.status, c.answered)
		if got != c.want || ok != c.ok {
			t.Errorf("carrierEnd(%q, %v) = %q, %v", c.status, c.answered, got, ok)
		}
	}
}

func TestSpeechTimeGrowsWithText(t *testing.T) {
	short, long := speechTime("Bye."), speechTime("Your verification code is 1 2 3 4 5 6. Again, 1 2 3 4 5 6.")
	if short < time.Second || long <= short || long > 30*time.Second {
		t.Fatalf("short=%v long=%v", short, long)
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
