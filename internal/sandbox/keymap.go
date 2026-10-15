package sandbox

import (
	"regexp"
	"strconv"
	"strings"
)

// DefaultKeymap is the guest keyboard layout when the host's cannot be read or has no XKB equivalent.
const DefaultKeymap = "us"

// keymapRE is what may travel to the guest as opt/orole/keymap: an XKB layout with an optional "(variant)". It also
// keeps QEMU's option syntax safe (no commas) and the guest's env file plain.
var keymapRE = regexp.MustCompile(`^[a-z0-9_()+-]{1,40}$`)

// ValidKeymap reports whether k may be passed to the guest.
func ValidKeymap(k string) bool { return keymapRE.MatchString(k) }

// klidKeymaps maps whole Windows keyboard layout identifiers (KLIDs, the 8 hex digits under
// HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Keyboard Layouts) to XKB layout(variant).
var klidKeymaps = map[uint32]string{
	0x00000409: "us",
	0x00010409: "us(dvorak)",
	0x00020409: "us(intl)", // United States-International
	0x00030409: "us(dvorak-l)",
	0x00040409: "us(dvorak-r)",
	0x00000809: "gb",
	0x00000452: "gb(extd)", // United Kingdom Extended
	0x00001809: "ie",
	0x0000080c: "be",        // Belgian French
	0x00000813: "be",        // Belgian (Period)
	0x0001080c: "be",        // Belgian (Comma)
	0x0000040c: "fr",        // French (legacy AZERTY)
	0x0001040c: "fr(afnor)", // French (Standard, AZERTY)
	0x0002040c: "fr(bepo_afnor)",
	0x0000100c: "ch(fr)", // Swiss French
	0x00000807: "ch",     // Swiss German
	0x00000413: "nl",
	0x00000407: "de",
	0x00010407: "de", // German (IBM)
	0x00000c07: "at",
	0x0000040a: "es",
	0x0001040a: "es", // Spanish Variation
	0x0000080a: "latam",
	0x00000410: "it",
	0x00010410: "it", // Italian (142)
	0x00000816: "pt",
	0x00000416: "br(abnt2)", // Portuguese (Brazil ABNT)
	0x00010416: "br(abnt2)",
	0x0000041d: "se",
	0x00000414: "no",
	0x00000406: "dk",
	0x0000040b: "fi",
	0x0000083b: "fi",         // Finnish with Sami
	0x00000415: "pl",         // Polish (Programmers)
	0x00010415: "pl(qwertz)", // Polish (214)
	0x00000405: "cz",
	0x00010405: "cz(qwerty)",
	0x0000041b: "sk",
	0x0000040e: "hu",
	0x00000424: "si",
	0x0000041a: "hr",
	0x00001009: "ca",            // Canadian French
	0x00000c0c: "ca(fr-legacy)", // Canadian French (Legacy)
	0x00011009: "ca(multix)",    // Canadian Multilingual Standard
	0x00000418: "ro",
	0x0000040f: "is",
	0x00000425: "ee",
	0x00000426: "lv",
	0x00000427: "lt",
	0x00000408: "gr",
	0x0000041f: "tr",
	0x00000422: "ua",
	0x00000419: "ru",
	0x00000402: "bg",
	0x0000040d: "il",
}

// langKeymaps maps the language identifier (the low 16 bits of a KLID) to a layout, for layouts not listed by KLID
// (IME layouts such as E0010411, substitutes with other high words). Only languages whose usual keyboard is clear.
var langKeymaps = map[uint16]string{
	0x0409: "us", 0x0809: "gb", 0x1809: "ie", 0x0c09: "us", 0x1409: "us", 0x1009: "us",
	0x080c: "be", 0x0813: "be", 0x040c: "fr", 0x100c: "ch(fr)", 0x140c: "fr", 0x0807: "ch",
	0x0413: "us", // Dutch (Netherlands) users mostly type on US International; the Dutch layout is by KLID
	0x0407: "de", 0x0c07: "at", 0x1007: "de",
	0x040a: "es", 0x0c0a: "es", 0x080a: "latam",
	0x0410: "it", 0x0816: "pt", 0x0416: "br(abnt2)",
	0x041d: "se", 0x0414: "no", 0x0814: "no", 0x0406: "dk", 0x040b: "fi",
	0x0415: "pl", 0x0405: "cz", 0x041b: "sk", 0x040e: "hu",
}

// KeymapForKLID returns the XKB layout(variant) for a Windows keyboard layout identifier such as "0000080C", or
// DefaultKeymap when it is unknown or malformed.
func KeymapForKLID(klid string) string {
	klid = strings.TrimSpace(klid)
	if len(klid) == 0 || len(klid) > 8 {
		return DefaultKeymap
	}
	v, err := strconv.ParseUint(klid, 16, 32)
	if err != nil {
		return DefaultKeymap
	}
	if k, ok := klidKeymaps[uint32(v)]; ok {
		return k
	}
	if k, ok := langKeymaps[uint16(v)]; ok {
		return k
	}
	return DefaultKeymap
}
