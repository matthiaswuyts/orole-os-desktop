// Package cliruntime finds, installs, updates and reads the sign-in state of the vendor CLIs that let Orole use
// a user's own Claude, ChatGPT (Codex) or Grok account. The CLI signs in and keeps the credentials; Orole never
// reads, stores or logs them. A managed copy lives in Orole's own store and is downloaded from the vendor's
// official release channel (hash-checked where the channel publishes a hash).
package cliruntime

// Provider ids.
const (
	Claude = "claude"
	Codex  = "codex"
	Grok   = "grok"
)

// Auth states.
const (
	SignedIn  = "signed_in"
	SignedOut = "signed_out"
	Unknown   = "unknown" // the CLI is missing, or has no way to ask
)

// Sources of the CLI that runs.
const (
	SourceOverride = "override" // OROLE_<ID>_PATH
	SourceManaged  = "managed"  // downloaded into Orole's store
	SourceSystem   = "system"   // found on PATH
)

// spec describes one provider CLI.
type spec struct {
	id, name string
	binary   string   // executable name without extension
	authArgs []string // prints the sign-in state; nil = the CLI cannot be asked
	login    []string
	logout   []string
}

var specs = []spec{
	{id: Claude, name: "Claude", binary: "claude", authArgs: []string{"auth", "status"}, login: []string{"auth", "login", "--claudeai"}, logout: []string{"auth", "logout"}},
	{id: Codex, name: "ChatGPT", binary: "codex", authArgs: []string{"login", "status"}, login: []string{"login"}, logout: []string{"logout"}},
	{id: Grok, name: "Grok", binary: "grok", login: []string{"login"}, logout: []string{"logout"}},
}

func specOf(id string) (spec, bool) {
	for _, s := range specs {
		if s.id == id {
			return s, true
		}
	}
	return spec{}, false
}

// Official release channels.
const (
	defaultNPM    = "https://registry.npmjs.org"
	defaultGitHub = "https://api.github.com/repos/openai/codex/releases/latest"
	defaultGrok   = "https://x.ai/cli"
)

// claudePlatform is the npm platform package suffix of Claude Code's native binary.
func claudePlatform(goos, goarch string) string {
	os := map[string]string{"windows": "win32", "darwin": "darwin", "linux": "linux"}[goos]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if os == "" || arch == "" {
		return ""
	}
	return os + "-" + arch
}

// codexTriple is the Rust target triple Codex's release assets are named after.
func codexTriple(goos, goarch string) string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if arch == "" {
		return ""
	}
	switch goos {
	case "windows":
		return arch + "-pc-windows-msvc"
	case "darwin":
		return arch + "-apple-darwin"
	case "linux":
		return arch + "-unknown-linux-musl"
	}
	return ""
}

// grokPlatform is the platform suffix of Grok's release binaries.
func grokPlatform(goos, goarch string) string {
	os := map[string]string{"windows": "windows", "darwin": "macos", "linux": "linux"}[goos]
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if os == "" || arch == "" {
		return ""
	}
	return os + "-" + arch
}
