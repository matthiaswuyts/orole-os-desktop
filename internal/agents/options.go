package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// LaunchOptions contains discovered machine-scoped choices, not an account model inventory.
type LaunchOptions struct {
	Machine           string            `json:"machine"`
	Harnesses         []HarnessOption   `json:"harnesses"`
	Projects          []ProjectOption   `json:"projects"`
	Directories       []DirectoryOption `json:"directories"`
	Worktree          bool              `json:"worktree"`
	UnavailableReason string            `json:"unavailableReason"`
}

type HarnessOption struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Available         bool     `json:"available"`
	Executable        string   `json:"executable"`
	Version           string   `json:"version"`
	Reasoning         []string `json:"reasoning"`
	CustomModel       bool     `json:"customModel"`
	Modes             []Mode   `json:"modes"`
	Structured        bool     `json:"structured"`
	Interactive       bool     `json:"interactive"`
	UnavailableReason string   `json:"unavailableReason"`
}

type ProjectOption struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Machine string `json:"machine"`
	Git     bool   `json:"git"`
}

type DirectoryOption struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Git  bool   `json:"git"`
}

func localMachine(machine string) bool { return machine == "" || machine == "local" }

// Codex config reference documents model_reasoning_effort; support still depends
// on the selected model. Claude's installed --help advertises --effort values.
// Neither list claims account/model availability.
func reasoningValues(kind string) []string {
	switch kind {
	case "claude":
		return []string{"low", "medium", "high", "xhigh", "max"}
	case "codex":
		return []string{"low", "medium", "high", "xhigh"}
	default:
		return []string{}
	}
}

func validateReasoning(kind, value string) error {
	if value != "" && !slices.Contains(reasoningValues(kind), value) {
		return fmt.Errorf("unsupported reasoning level %q for %s", value, kind)
	}
	return nil
}

func validateModel(value string) error {
	if len(value) > 128 || !utf8.ValidString(value) {
		return errors.New("model must be valid text of at most 128 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("model cannot contain control or formatting characters")
		}
	}
	return nil
}

func advertisedReasoning(kind, help string) []string {
	out := []string{}
	if kind == "codex" && strings.Contains(help, "--config") {
		return reasoningValues(kind)
	}
	if kind == "claude" {
		start := strings.Index(help, "--effort")
		if start < 0 {
			return out
		}
		section := help[start:]
		if end := strings.Index(section[2:], "--"); end >= 0 {
			section = section[:end+2]
		}
		for _, value := range reasoningValues(kind) {
			for _, word := range strings.FieldsFunc(section, func(r rune) bool { return !unicode.IsLetter(r) }) {
				if word == value {
					out = append(out, value)
					break
				}
			}
		}
	}
	return out
}

func localHelp(ctx context.Context, executable string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "--help")
	hideWindow(cmd)
	out := &tailBuf{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = time.Second
	if cmd.Run() != nil {
		return ""
	}
	return out.String()
}

func (m *Manager) Options(ctx context.Context, machine string) (LaunchOptions, error) {
	if machine == "" {
		machine = "local"
	}
	out := LaunchOptions{Machine: machine, Harnesses: []HarnessOption{}, Projects: []ProjectOption{}, Directories: []DirectoryOption{}}
	if localMachine(machine) {
		out.Worktree = m.cfg.Worktrees != nil
		for _, d := range m.Detect(ctx, false) {
			h := HarnessOption{ID: d.Kind, Name: d.Name, Available: d.Installed, Executable: d.Path, Version: d.Version, Modes: d.Modes, Reasoning: []string{}, CustomModel: d.Kind != "custom", Structured: slices.Contains(d.Modes, ModeStructured), Interactive: slices.Contains(d.Modes, ModeTerminal), UnavailableReason: d.Error}
			if d.Installed && (d.Kind == "claude" || d.Kind == "codex") {
				h.Reasoning = advertisedReasoning(d.Kind, localHelp(ctx, d.Path))
			}
			out.Harnesses = append(out.Harnesses, h)
		}
		if m.cfg.ProjectOptions != nil {
			projects, err := m.cfg.ProjectOptions(ctx)
			if err != nil {
				return out, err
			}
			for _, p := range projects {
				if localMachine(p.Machine) {
					out.Projects = append(out.Projects, p)
				}
			}
		}
		if m.cfg.Machines != nil {
			box, err := m.cfg.Machines.Get(ctx, machine)
			if err != nil {
				return out, err
			}
			for _, root := range box.Roots {
				if info, err := os.Stat(root); err == nil && info.IsDir() {
					_, gitErr := os.Stat(filepath.Join(root, ".git"))
					out.Directories = append(out.Directories, DirectoryOption{Path: root, Name: filepath.Base(root), Git: gitErr == nil})
				}
			}
		}
		return out, nil
	}
	if m.cfg.Machines == nil {
		return out, errors.New("machine transport is unavailable")
	}
	env, err := m.cfg.Machines.AgentOptions(ctx, "user", machine)
	if err != nil {
		out.UnavailableReason = err.Error()
		return out, nil
	}
	for _, d := range env.Executables {
		reason := ""
		if d.Path == "" {
			reason = "Not installed on this box"
		}
		if isBatch(d.Path) {
			reason = "Remote Windows requires a native CLI executable on PATH, not a cmd/bat shim"
		}
		if d.Path != "" && ((d.Kind == "claude" && (!strings.Contains(d.Help, "--print") || !strings.Contains(d.Help, "--output-format"))) || (d.Kind == "codex" && (!strings.Contains(d.Help, "exec") || !strings.Contains(d.Help, "--config")))) {
			reason = "Installed CLI does not advertise the required remote process flags"
		}
		h := HarnessOption{ID: d.Kind, Name: m.AdapterName(d.Kind), Available: d.Path != "" && reason == "", Executable: d.Path, Version: d.Version, Reasoning: advertisedReasoning(d.Kind, d.Help), CustomModel: true, Modes: []Mode{ModeProcess}, UnavailableReason: reason}
		out.Harnesses = append(out.Harnesses, h)
	}
	for _, d := range env.Directories {
		out.Directories = append(out.Directories, DirectoryOption{Path: d.Path, Name: path.Base(d.Path), Git: d.Git})
		if d.Git {
			out.Projects = append(out.Projects, ProjectOption{ID: machine + ":" + d.Path, Name: path.Base(d.Path), Path: d.Path, Machine: machine, Git: true})
		}
	}
	return out, nil
}
