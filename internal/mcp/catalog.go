package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

const (
	maxToolName    = 128
	maxDescription = 4096
	maxSchemaBytes = 64 << 10
	maxCatalogList = 2000
)

var toolNameRE = regexp.MustCompile(`^[A-Za-z0-9_.\-/]{1,128}$`)

func validToolName(n string) bool { return toolNameRE.MatchString(n) }

// classify derives our effect class from a tool's annotations. Annotations are
// claims by an untrusted server: they only choose how we ask, never lift an
// approval requirement (see policy.Request.FromHint).
func classify(readOnly bool, destructive *bool) policy.Effect {
	switch {
	case readOnly:
		return policy.EffectRead
	case destructive == nil || *destructive: // MCP defaults destructiveHint to true
		return policy.EffectDestructive
	}
	return policy.EffectWrite
}

func rawJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxSchemaBytes {
		return nil
	}
	return untrusted.JSON(b, maxSchemaBytes)
}

func convertTool(t *sdk.Tool) (ToolInfo, bool) {
	if t == nil || !validToolName(t.Name) {
		return ToolInfo{}, false
	}
	ti := ToolInfo{
		Name:         t.Name,
		Description:  untrusted.Text(t.Description, maxDescription),
		InputSchema:  rawJSON(t.InputSchema),
		OutputSchema: rawJSON(t.OutputSchema),
		Title:        untrusted.Line(t.Title, 200),
	}
	ro := false
	var destructive *bool
	if a := t.Annotations; a != nil {
		ro = a.ReadOnlyHint
		destructive = a.DestructiveHint
		ti.ReadOnlyHint = new(a.ReadOnlyHint)
		ti.DestructiveHint = a.DestructiveHint
		ti.IdempotentHint = new(a.IdempotentHint)
		ti.OpenWorldHint = a.OpenWorldHint
		if ti.Title == "" {
			ti.Title = untrusted.Line(a.Title, 200)
		}
	}
	ti.Effect = string(classify(ro, destructive))
	return ti, true
}

// buildCatalog lists tools, resources and prompts of a live session. Capability
// absence is not an error; a failing list call is returned but partial data is kept.
func buildCatalog(ctx context.Context, id string, sess *sdk.ClientSession) (*Catalog, error) {
	c := &Catalog{ServerID: id, FetchedAt: time.Now().Unix()}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if ir := sess.InitializeResult(); ir != nil {
		c.ProtocolVersion = untrusted.Line(ir.ProtocolVersion, 32)
		if ir.ServerInfo != nil {
			c.ServerName = untrusted.Line(ir.ServerInfo.Name, 120)
			c.ServerVersion = untrusted.Line(ir.ServerInfo.Version, 64)
		}
		c.Capabilities = rawJSON(ir.Capabilities)
	}
	caps := sess.InitializeResult().Capabilities
	if caps == nil || caps.Tools != nil {
		for t, err := range sess.Tools(ctx, nil) {
			if err != nil {
				keep(err)
				break
			}
			if len(c.Tools) >= maxCatalogList {
				break
			}
			if ti, ok := convertTool(t); ok {
				c.Tools = append(c.Tools, ti)
			} else {
				c.Skipped++
			}
		}
	}
	if caps != nil && caps.Resources != nil {
		for r, err := range sess.Resources(ctx, nil) {
			if err != nil {
				keep(err)
				break
			}
			if len(c.Resources) >= maxCatalogList {
				break
			}
			c.Resources = append(c.Resources, ResourceInfo{URI: untrusted.Line(r.URI, 1024), Name: untrusted.Line(r.Name, 200),
				Description: untrusted.Text(r.Description, 1024), MimeType: untrusted.Line(r.MIMEType, 120)})
		}
		for r, err := range sess.ResourceTemplates(ctx, nil) {
			if err != nil {
				keep(err)
				break
			}
			if len(c.Resources) >= maxCatalogList {
				break
			}
			c.Resources = append(c.Resources, ResourceInfo{URI: untrusted.Line(r.URITemplate, 1024), Name: untrusted.Line(r.Name, 200),
				Description: untrusted.Text(r.Description, 1024), MimeType: untrusted.Line(r.MIMEType, 120), Template: true})
		}
	}
	if caps != nil && caps.Prompts != nil {
		for p, err := range sess.Prompts(ctx, nil) {
			if err != nil {
				keep(err)
				break
			}
			if len(c.Prompts) >= maxCatalogList {
				break
			}
			pi := PromptInfo{Name: untrusted.Line(p.Name, 200), Description: untrusted.Text(p.Description, 1024)}
			for _, a := range p.Arguments {
				pi.Arguments = append(pi.Arguments, PromptArg{Name: untrusted.Line(a.Name, 120), Description: untrusted.Text(a.Description, 512), Required: a.Required})
			}
			c.Prompts = append(c.Prompts, pi)
		}
	}
	return c, firstErr
}
