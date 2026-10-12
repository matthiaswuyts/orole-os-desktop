// Command gen downloads models.dev and writes the trimmed price snapshot embedded by
// internal/pricing (run through `go generate ./internal/pricing`).
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/pricing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Get(pricing.SourceURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models.dev: %s", resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	out, err := pricing.Trim(raw, time.Now())
	if err != nil {
		return err
	}
	return os.WriteFile("snapshot.json", append(out, '\n'), 0o644)
}
