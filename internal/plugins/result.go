package plugins

import (
	"strings"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

const maxResultBytes = 128 << 10

// Only text content crosses the plugin boundary. Structured objects, resource
// links and binary data are deliberately not forwarded or interpreted.
func plainResult(result *mcp.CallResult) PluginRunResult {
	out := PluginRunResult{IsError: result.IsError}
	var text strings.Builder
	for _, block := range result.Content {
		out.Truncated = out.Truncated || block.Truncated
		if (block.Type != "text" && block.Type != "resource") || block.Text == "" {
			continue
		}
		if text.Len() == maxResultBytes {
			out.Truncated = true
			break
		}
		clean := untrusted.Text(block.Text, maxResultBytes)
		if clean == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		remaining := maxResultBytes - text.Len()
		if len(clean) > remaining {
			cut := remaining
			for cut > 0 && !utf8.RuneStart(clean[cut]) {
				cut--
			}
			text.WriteString(clean[:cut])
			out.Truncated = true
			break
		}
		text.WriteString(clean)
	}
	out.Text = text.String()
	return out
}
