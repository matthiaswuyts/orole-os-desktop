package cliruntime

import (
	"context"
	"os/exec"
)

// Runner runs a CLI to completion and returns its combined output. A non-zero exit is an error, but the output
// is still returned: some CLIs print their answer ("Not logged in") and then exit 1.
type Runner func(ctx context.Context, path string, args ...string) (string, error)

func execRunner(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
