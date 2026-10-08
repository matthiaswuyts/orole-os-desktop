// Package skills is Orole's central skill registry: one set of skills, kept in the daemon's data
// directory, that every agent harness sees. Harnesses with a native skills directory get synced
// copies; every structured harness also reaches the skills through the Orole MCP gateway.
//
// A skill is a directory <DataDir>/skills/<name>/ holding SKILL.md (YAML frontmatter with name and
// description, then Markdown instructions) and optional resource files.
package skills

// Skill is one registry entry.
type Skill struct {
	Name        string   `json:"name"`        // slug, also the directory name
	Description string   `json:"description"` // frontmatter description
	Body        string   `json:"body"`        // full SKILL.md text including frontmatter
	Files       []string `json:"files"`       // resource files next to SKILL.md, relative paths
	Enabled     bool     `json:"enabled"`     // disabled skills are neither synced nor listed to agents
	Source      string   `json:"source"`      // "orole" or "imported:<harness>"
	UpdatedAt   int64    `json:"updatedAt"`
}

// Discovered is a skill found in a harness's own skills directory that the registry does not hold.
type Discovered struct {
	Harness     string `json:"harness"` // claude | codex | omp
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"` // directory containing SKILL.md
}

// SyncState is one harness's view of the registry.
type SyncState struct {
	Harness   string   `json:"harness"`   // claude | codex | omp
	Name      string   `json:"name"`      // display name
	Available bool     `json:"available"` // the harness is installed
	Dir       string   `json:"dir"`       // native skills directory skills are synced into
	Synced    []string `json:"synced"`    // skill names present and current
	Outdated  []string `json:"outdated"`  // present but differing from the registry
	Missing   []string `json:"missing"`   // enabled in the registry, absent in the harness
	Gateway   string   `json:"gateway"`   // "installed" | "outdated" | "missing" | "unsupported" (the "orole" MCP entry)
	Error     string   `json:"error"`
}
