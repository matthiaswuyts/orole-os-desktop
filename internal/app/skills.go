package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// Skills is the bound service for the central skill registry. Every harness sees these skills:
// synced into native skills directories, and served through the Orole MCP gateway.
type Skills struct{ L *Link }

// List returns every registry skill.
func (s Skills) List(ctx context.Context) ([]skills.Skill, error) {
	var out []skills.Skill
	err := s.L.call(ctx, "skills.list", struct{}{}, &out)
	return out, err
}

// Get returns one skill with its full SKILL.md.
func (s Skills) Get(ctx context.Context, name string) (skills.Skill, error) {
	var out skills.Skill
	err := s.L.call(ctx, "skills.get", map[string]string{"name": name}, &out)
	return out, err
}

// Save creates or replaces a skill from SKILL.md text (frontmatter name wins over the argument).
func (s Skills) Save(ctx context.Context, name, body string) (skills.Skill, error) {
	var out skills.Skill
	err := s.L.call(ctx, "skills.save", map[string]string{"name": name, "body": body}, &out)
	return out, err
}

// Delete removes a skill from the registry (native copies are removed on the next sync).
func (s Skills) Delete(ctx context.Context, name string) error {
	return s.L.call(ctx, "skills.delete", map[string]string{"name": name}, nil)
}

// SetEnabled turns a skill on or off for every harness.
func (s Skills) SetEnabled(ctx context.Context, name string, enabled bool) (skills.Skill, error) {
	var out skills.Skill
	err := s.L.call(ctx, "skills.setEnabled", map[string]any{"name": name, "enabled": enabled}, &out)
	return out, err
}

// Discover lists skills found in harness skill directories that the registry does not hold yet.
func (s Skills) Discover(ctx context.Context) ([]skills.Discovered, error) {
	var out []skills.Discovered
	err := s.L.call(ctx, "skills.discover", struct{}{}, &out)
	return out, err
}

// Import copies a discovered skill into the registry.
func (s Skills) Import(ctx context.Context, harness, name string) (skills.Skill, error) {
	var out skills.Skill
	err := s.L.call(ctx, "skills.import", map[string]string{"harness": harness, "name": name}, &out)
	return out, err
}

// SyncState reports, per harness, which skills and the Orole MCP gateway are present and current.
func (s Skills) SyncState(ctx context.Context) ([]skills.SyncState, error) {
	var out []skills.SyncState
	err := s.L.call(ctx, "skills.syncState", struct{}{}, &out)
	return out, err
}

// Sync writes the registry into one harness's native directories ("" = every installed harness).
func (s Skills) Sync(ctx context.Context, harness string) ([]skills.SyncState, error) {
	var out []skills.SyncState
	err := s.L.call(ctx, "skills.sync", map[string]string{"harness": harness}, &out)
	return out, err
}

// HubTopics lists skills.sh's curated topics (no network access).
func (s Skills) HubTopics(ctx context.Context) ([]skills.HubTopic, error) {
	var out []skills.HubTopic
	err := s.L.call(ctx, "skills.hub.topics", struct{}{}, &out)
	return out, err
}

// HubTopic lists the skills of one skills.sh topic.
func (s Skills) HubTopic(ctx context.Context, id string) (skills.HubPage, error) {
	var out skills.HubPage
	err := s.L.call(ctx, "skills.hub.topic", map[string]string{"id": id}, &out)
	return out, err
}

// HubBrowse lists one page of the skills.sh catalogue; view is "all-time" or "trending", pages start at 0.
func (s Skills) HubBrowse(ctx context.Context, view string, page int) (skills.HubPage, error) {
	var out skills.HubPage
	err := s.L.call(ctx, "skills.hub.browse", map[string]any{"view": view, "page": page}, &out)
	return out, err
}

// HubSearch searches the skills.sh catalogue by name (at least two characters).
func (s Skills) HubSearch(ctx context.Context, query string) (skills.HubPage, error) {
	var out skills.HubPage
	err := s.L.call(ctx, "skills.hub.search", map[string]string{"query": query}, &out)
	return out, err
}

// HubDescribe returns the descriptions skills.sh shows for up to 40 skills ("" when unknown).
func (s Skills) HubDescribe(ctx context.Context, refs []skills.HubRef) ([]skills.HubDescription, error) {
	var out []skills.HubDescription
	err := s.L.call(ctx, "skills.hub.describe", map[string]any{"refs": refs}, &out)
	return out, err
}

// HubPreview downloads a skill and returns its full SKILL.md and file list. Nothing is added.
func (s Skills) HubPreview(ctx context.Context, source, skillID string) (skills.HubPreview, error) {
	var out skills.HubPreview
	err := s.L.call(ctx, "skills.hub.preview", skills.HubRef{Source: source, SkillID: skillID}, &out)
	return out, err
}

// HubInstall adds a skills.sh skill to the registry. Harness folders only change on the next Sync.
func (s Skills) HubInstall(ctx context.Context, source, skillID string) (skills.Skill, error) {
	var out skills.Skill
	err := s.L.call(ctx, "skills.hub.install", skills.HubRef{Source: source, SkillID: skillID}, &out)
	return out, err
}

// Images is the bound service for image generation through installed harnesses (Codex).
type Images struct{ L *Link }

// Start begins an image-generation task; generated images arrive as image events on the session.
func (i Images) Start(ctx context.Context, prompt string) (agents.Session, error) {
	var out agents.Session
	err := i.L.call(ctx, "images.start", map[string]string{"prompt": prompt}, &out)
	return out, err
}

// Artifact returns a file a session produced (an image), validated to live in the session's
// scratch directory.
func (a Agents) Artifact(ctx context.Context, id, path string) (agents.Artifact, error) {
	var out agents.Artifact
	err := a.L.call(ctx, "agents.artifact", map[string]string{"id": id, "path": path}, &out)
	return out, err
}

// Publish commits, pushes and opens a pull request for a session's branch.
func (a Agents) Publish(ctx context.Context, id string, spec agents.PublishSpec) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.publish", map[string]any{"id": id, "spec": spec}, &out)
	return out, err
}
