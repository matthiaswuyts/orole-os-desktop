// Package oroleos exposes the release version shared by the desktop app, the daemon and the CLI.
package oroleos

import (
	_ "embed"
	"encoding/json"
)

//go:embed mygo.json
var manifest []byte

// Version is the `version` in mygo.json, the same value MyGo stamps into the desktop app.
// scripts/build.ps1 -Version overrides it with -ldflags "-X github.com/rayorole/orole-os-desktop.Version=...".
var Version string

func init() {
	if Version != "" {
		return
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil || m.Version == "" {
		panic("mygo.json has no version")
	}
	Version = m.Version
}
