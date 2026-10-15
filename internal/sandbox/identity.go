package sandbox

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// DefaultGuestUser is the account name of a bot computer whose bot has no usable name, and what the guest image
// falls back to when the host passes none (guest/configuration.nix).
const DefaultGuestUser = "bot"

const maxGuestUser = 24

// guestUserRE is what may travel to the guest as opt/orole/user; the guest checks the same pattern.
var guestUserRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)

// ValidGuestUser reports whether u may be passed to the guest as its account name.
func ValidGuestUser(u string) bool { return guestUserRE.MatchString(u) && !strings.HasSuffix(u, "-") }

// reservedGuestUsers are system accounts of the guest image (and common Linux ones) a bot's name must not take.
var reservedGuestUsers = map[string]bool{
	"root": true, "daemon": true, "bin": true, "sys": true, "nobody": true, "adm": true, "wheel": true,
	"users": true, "nogroup": true, "messagebus": true, "sshd": true, "polkituser": true, "polkitd": true,
	"systemd": true, "dbus": true, "nscd": true, "rtkit": true, "avahi": true, "sddm": true, "lp": true,
	"mail": true, "news": true, "uucp": true, "operator": true, "games": true, "ftp": true, "sync": true,
	"shutdown": true, "halt": true, "tty": true, "disk": true, "kmem": true, "input": true, "audio": true,
	"video": true, "render": true, "kvm": true, "utmp": true, "uidmap": true, "keys": true, "dialout": true,
	"cdrom": true, "floppy": true, "tape": true, "lock": true, "log": true, "shadow": true, "staff": true,
	"backup": true, "list": true, "irc": true, "gnats": true, "man": true, "proxy": true, "www-data": true,
	"nixbld": true, "orole": true, "orole-guest": true,
}

func reservedGuestUser(u string) bool {
	return reservedGuestUsers[u] || strings.HasPrefix(u, "systemd-") || strings.HasPrefix(u, "nixbld")
}

// GuestUser maps a bot's display name to the Linux account name of its computer: accents removed, lowercase, runs
// of anything outside a-z and 0-9 become "-", at most 24 characters, starting with a letter ("bot-" is put in front
// otherwise), and "-bot" appended to a system account's name. "Atlas 2" is atlas-2, "Élodie" elodie, "42" bot-42,
// and a name with nothing usable is "bot".
func GuestUser(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(name) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	u := b.String()
	if u == "" {
		return DefaultGuestUser
	}
	if u[0] < 'a' || u[0] > 'z' {
		u = DefaultGuestUser + "-" + u
	}
	u = trimGuestUser(u, maxGuestUser)
	if reservedGuestUser(u) {
		u = trimGuestUser(u, maxGuestUser-len("-bot")) + "-bot"
	}
	return u
}

func trimGuestUser(u string, n int) string {
	if len(u) > n {
		u = u[:n]
	}
	return strings.TrimRight(u, "-")
}

// GuestHome is the home folder of the computer's account (the guest bind-mounts its stable home there).
func GuestHome(user string) string { return "/home/" + user }

// GuestHostname is the computer's host name for an account name.
func GuestHostname(user string) string { return user + "-computer" }
