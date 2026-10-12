// fakeclaudemodels implements only the quota-free Claude SDK initialization exchange.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

func main() {
	if slices.Contains(os.Args, "--version") {
		fmt.Println("1.0.0 (Claude Code fixture)")
		return
	}
	if slices.Contains(os.Args, "--help") {
		fmt.Println("--effort <level> low medium high xhigh max")
		return
	}
	for _, flag := range []string{"-p", "--input-format", "--output-format", "--verbose", "--no-session-persistence", "--safe-mode", "--strict-mcp-config", "--tools", "--settings"} {
		if !slices.Contains(os.Args, flag) {
			panic("missing safety flag: " + flag)
		}
	}
	cwd, _ := os.Getwd()
	if os.Getenv("DISABLE_AUTOUPDATER") != "1" || os.Getenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC") != "1" {
		panic("discovery may update or phone home")
	}
	// The catalog is the signed-in account's, so discovery must read the user's own configuration.
	if os.Getenv("CLAUDE_CONFIG_DIR") != os.Getenv("FAKE_CLAUDE_ACCOUNT_CONFIG") {
		panic("discovery does not read the signed-in account")
	}
	if cwd == os.Getenv("CLAUDE_CONFIG_DIR") {
		panic("discovery runs inside the user's configuration")
	}
	input, _ := io.ReadAll(os.Stdin)
	var request struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if json.Unmarshal(input, &request) != nil || request.Type != "control_request" || request.Request.Subtype != "initialize" {
		panic("expected only one initialize request, never a user turn")
	}
	if log := os.Getenv("FAKE_CLAUDE_MODELS_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(f, cwd)
		f.Close()
	}
	switch os.Getenv("FAKE_CLAUDE_MODELS_MODE") {
	case "hang":
		time.Sleep(time.Minute)
		return
	case "eof":
		return
	case "error":
		fmt.Printf(`{"type":"control_response","response":{"subtype":"error","request_id":%q,"error":"unavailable"}}`+"\n", request.RequestID)
		return
	}
	fmt.Println(`{"type":"system","subtype":"ui_invalidate"}`)
	fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"models":[{"value":"default","resolvedModel":"claude-opus-5-5","displayName":"Default (recommended)","supportedEffortLevels":["low","high"]},{"value":"opus","resolvedModel":"claude-opus-5-5","displayName":"Opus","supportedEffortLevels":["low","high"]},{"value":"haiku","resolvedModel":"claude-haiku-4-5-20251001","displayName":"Haiku"}]}}}`+"\n", request.RequestID)
	// The caller must stop the SDK after receiving metadata, not wait for a turn.
	time.Sleep(time.Minute)
}
