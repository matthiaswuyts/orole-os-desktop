package sandbox

import (
	"strings"
	"testing"
)

func TestGuestUser(t *testing.T) {
	tests := []struct{ name, want string }{
		{"Atlas 2", "atlas-2"}, {"Élodie", "elodie"}, {"42", "bot-42"}, {"Élodie Test", "elodie-test"},
		{"", "bot"}, {"   ", "bot"}, {"!!!", "bot"}, {"日本", "bot"}, {"Zoë's   Helper!", "zoe-s-helper"},
		{"--Jarvis--", "jarvis"}, {"Crème Brûlée", "creme-brulee"}, {"ÅSA_ÖSTER", "asa-oster"},
		{"Root", "root-bot"}, {"nobody", "nobody-bot"}, {"systemd-timesync", "systemd-timesync-bot"},
		{"nixbld1", "nixbld1-bot"}, {"Messagebus", "messagebus-bot"}, {"bot", "bot"},
		{"A very long assistant name indeed", "a-very-long-assistant-na"},
		{"abcdefghijklmnopqrstuvw x", "abcdefghijklmnopqrstuvw"}, // the cut lands on a dash, which is trimmed
		{"systemd-abcdefghijklmnopqrstuvwxyz", "systemd-abcdefghijkl-bot"},
		{"7 Seas", "bot-7-seas"},
	}
	for _, tt := range tests {
		got := GuestUser(tt.name)
		if got != tt.want {
			t.Errorf("GuestUser(%q) = %q, want %q", tt.name, got, tt.want)
		}
		if !ValidGuestUser(got) {
			t.Errorf("GuestUser(%q) = %q is not a valid guest user", tt.name, got)
		}
		if h := GuestHostname(got); len(h) > 63 {
			t.Errorf("hostname %q too long", h)
		}
	}
}

func TestValidGuestUser(t *testing.T) {
	for _, u := range []string{"bot", "a", "elodie-test", strings.Repeat("a", 24)} {
		if !ValidGuestUser(u) {
			t.Errorf("%q refused", u)
		}
	}
	for _, u := range []string{"", "1bot", "Bot", "bot-", "-bot", "a b", "a,b", "a_b", strings.Repeat("a", 25), "é"} {
		if ValidGuestUser(u) {
			t.Errorf("%q accepted", u)
		}
	}
}

func TestGuestHomeAndHostname(t *testing.T) {
	if GuestHome("atlas-2") != "/home/atlas-2" || GuestHostname("atlas-2") != "atlas-2-computer" {
		t.Fatal(GuestHome("atlas-2"), GuestHostname("atlas-2"))
	}
}
