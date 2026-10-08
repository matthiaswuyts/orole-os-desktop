package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const claudeModelsRequestID = "orole-models"

// Context-window labels such as "(1M context)" are not model versions.
var claudeModelVersion = regexp.MustCompile(`\b\d+(?:\.\d+)*\b`)

var claudeModelCache struct {
	mu     sync.Mutex
	path   string
	at     time.Time
	models []ModelOption
}

// claudeModels reads the installed CLI's catalog, not account entitlements. SDK
// initialization requires no user message or model turn. An isolated config and
// working directory keep startup writes out of the user's Claude configuration.
func claudeModels(ctx context.Context, bin string) ([]ModelOption, error) {
	claudeModelCache.mu.Lock()
	defer claudeModelCache.mu.Unlock()
	if claudeModelCache.path == bin && time.Since(claudeModelCache.at) < 5*time.Minute {
		return slices.Clone(claudeModelCache.models), nil
	}
	models, err := listClaudeModels(ctx, bin)
	if err != nil {
		return nil, err
	}
	claudeModelCache.path, claudeModelCache.at, claudeModelCache.models = bin, time.Now(), slices.Clone(models)
	return models, nil
}

func listClaudeModels(ctx context.Context, bin string) ([]ModelOption, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "orole-claude-models-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, bin, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--no-session-persistence", "--safe-mode", "--strict-mcp-config", "--tools", "", "--settings", `{"disableAllHooks":true}`)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+dir, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1")
	cmd.Stdin = strings.NewReader(`{"type":"control_request","request_id":"` + claudeModelsRequestID + `","request":{"subtype":"initialize"}}` + "\n")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	hideWindow(cmd)
	group, err := newJob()
	if err != nil {
		return nil, err
	}
	defer group.close()
	cmd.Cancel = func() error {
		group.kill()
		return cmd.Process.Kill()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		group.kill()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if err := group.assign(cmd.Process.Pid); err != nil {
		return nil, err
	}
	models, err := readClaudeModels(stdout)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return models, err
}

// The initialize response embeds models alongside commands, account metadata and
// other SDK capabilities. Only consume our response, never send a user prompt.
func readClaudeModels(r io.Reader) ([]ModelOption, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var msg struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Error     string `json:"error"`
				Response  struct {
					Models []struct {
						Value                 string   `json:"value"`
						ResolvedModel         string   `json:"resolvedModel"`
						DisplayName           string   `json:"displayName"`
						SupportedEffortLevels []string `json:"supportedEffortLevels"`
						Disabled              bool     `json:"disabled"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return nil, fmt.Errorf("Claude initialize response: %w", err)
		}
		if msg.Type != "control_response" || msg.Response.RequestID != claudeModelsRequestID {
			continue
		}
		if msg.Response.Subtype != "success" {
			return nil, fmt.Errorf("Claude initialize: %s", msg.Response.Error)
		}
		models := make([]ModelOption, 0, len(msg.Response.Response.Models))
		for _, m := range msg.Response.Response.Models {
			if m.Disabled || m.Value == "" || validateModel(m.Value) != nil {
				continue
			}
			name := m.DisplayName
			if name == "" {
				name = m.Value
			}
			// Newer SDKs return an unversioned label plus the authoritative resolved
			// ID. Older SDKs include the version directly in displayName.
			if m.ResolvedModel != "" && !claudeModelVersion.MatchString(name) {
				name += " (" + m.ResolvedModel + ")"
			}
			levels := make([]string, 0, len(m.SupportedEffortLevels))
			for _, level := range m.SupportedEffortLevels {
				if level != "" && validateReasoning("claude", level) == nil && !slices.Contains(levels, level) {
					levels = append(levels, level)
				}
			}
			models = append(models, ModelOption{ID: m.Value, Name: name, Reasoning: levels, Default: m.Value == "default"})
		}
		return models, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Claude initialize response: %w", err)
	}
	return nil, errors.New("Claude initialize returned no model catalog")
}
