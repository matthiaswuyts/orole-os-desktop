package sandbox

import "testing"

func TestKeymapForKLID(t *testing.T) {
	tests := []struct{ klid, want string }{
		{"0000080C", "be"}, {"0000080c", "be"}, {"00000813", "be"}, {"0001080C", "be"},
		{"0000040C", "fr"}, {"00000413", "nl"}, {"00000407", "de"}, {"00000807", "ch"}, {"0000100C", "ch(fr)"},
		{"00000409", "us"}, {"00000809", "gb"}, {"0000040A", "es"}, {"00000410", "it"}, {"00000816", "pt"},
		{"00000416", "br(abnt2)"}, {"0000041D", "se"}, {"00000414", "no"}, {"00000406", "dk"}, {"0000040B", "fi"},
		{"00000415", "pl"}, {"00000405", "cz"}, {"00020409", "us(intl)"}, {"00010409", "us(dvorak)"},
		// Unknown KLID: the language decides; IME layouts and substitutes carry the language in the low word.
		{"D001080C", "be"}, {"E0010407", "de"}, {"0000080D", "us"},
		// Unknown language and malformed input fall back to US.
		{"E0010411", "us"}, {"", "us"}, {"zzzz", "us"}, {"123456789", "us"}, {" 0000040C ", "fr"},
	}
	for _, tt := range tests {
		if got := KeymapForKLID(tt.klid); got != tt.want {
			t.Errorf("KeymapForKLID(%q) = %q, want %q", tt.klid, got, tt.want)
		}
	}
}

func TestEveryMappedKeymapIsValid(t *testing.T) {
	for klid, k := range klidKeymaps {
		if !ValidKeymap(k) {
			t.Errorf("KLID %08x maps to invalid %q", klid, k)
		}
	}
	for lang, k := range langKeymaps {
		if !ValidKeymap(k) {
			t.Errorf("language %04x maps to invalid %q", lang, k)
		}
	}
}

func TestValidKeymap(t *testing.T) {
	for _, k := range []string{"us", "be", "ch(fr)", "us(dvorak-l)", "fr(bepo_afnor)", "de+nodeadkeys"} {
		if !ValidKeymap(k) {
			t.Errorf("%q refused", k)
		}
	}
	for _, k := range []string{"", "US", "be,x", "a b", "fr\n", "us;rm", "../x", "x=y", string(make([]byte, 41))} {
		if ValidKeymap(k) {
			t.Errorf("%q accepted", k)
		}
	}
}
