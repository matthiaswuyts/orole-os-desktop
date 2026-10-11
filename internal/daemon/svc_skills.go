package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/rayorole/orole-os-desktop/internal/gateway"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// Central skills and the MCP gateway. Skills live in <DataDir>/skills (internal/skills); every
// harness sees them through native-directory sync and through the `orole` MCP gateway, which
// also proxies every enabled, connected MCP server (internal/gateway).
//
// IPC methods: skills.list/get/save/delete/setEnabled/discover/import/syncState/sync and
// gateway.tools/call/skills/skill. Event: skills.changed ({action, name}).
//
// gateway.call goes through MCP.InvokeChecked: policy decision, user approval and audit run
// exactly as for the UI's own tool calls, with the actor agent:<sessionID>. The pipe is
// per-user and token-authenticated, so a caller that can reach gateway.call can already call
// mcp.call as "user"; the session id only attributes the call for policy rules and audit.

func (d *Daemon) registerSkills() {
	if exe, err := os.Executable(); err == nil {
		d.gatewayExe = exe
	}
	store, err := skills.NewStore(d.DataDir)
	if err != nil {
		d.Log.Error("skills registry unavailable", "err", err)
	} else {
		if err := store.EnsureBuiltin(); err != nil {
			d.Log.Warn("built-in skills", "err", err)
		}
		d.Skills = store
		d.Gateway = &gateway.Hub{MCP: d.MCP, Skills: store}
	}
	d.registerSkillHandlers()
}

func skillErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, skills.ErrInvalid):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, skills.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	}
	return err
}

func (d *Daemon) skillStore() (*skills.Store, error) {
	if d.Skills == nil {
		return nil, errors.New("skills registry is unavailable")
	}
	return d.Skills, nil
}

// skillsPrompt is the enabled-skills index appended to structured harnesses' instructions.
func (d *Daemon) skillsPrompt() string {
	if d.Skills == nil {
		return ""
	}
	return d.Skills.PromptIndex()
}

func (d *Daemon) syncer() (*skills.Syncer, error) {
	st, err := d.skillStore()
	if err != nil {
		return nil, err
	}
	loc, err := skills.DefaultLocations()
	if err != nil {
		return nil, err
	}
	return &skills.Syncer{Store: st, Loc: loc, GatewayExe: d.gatewayExe, DataDir: d.DataDir}, nil
}

func (d *Daemon) skillsChanged(action, name string) {
	d.IPC.Publish("skills.changed", map[string]string{"action": action, "name": name})
}

func (d *Daemon) registerSkillHandlers() {
	type nameArg struct {
		Name string `json:"name"`
	}
	// mutate runs a registry change, audits it and tells subscribers.
	mutate := func(action, name string, f func(*skills.Store) (skills.Skill, error)) (skills.Skill, error) {
		st, err := d.skillStore()
		if err != nil {
			return skills.Skill{}, err
		}
		sk, err := f(st)
		if err != nil {
			d.audit("skills."+action, name, "failed", err.Error())
			return skills.Skill{}, skillErr(err)
		}
		d.audit("skills."+action, sk.Name, "allowed", sk.Source)
		d.skillsChanged(action, sk.Name)
		return sk, nil
	}

	handle(d, "skills.list", func(ctx context.Context, _ none) ([]skills.Skill, error) {
		st, err := d.skillStore()
		if err != nil {
			return nil, err
		}
		list, err := st.List()
		if list == nil {
			list = []skills.Skill{}
		}
		return list, skillErr(err)
	})
	handle(d, "skills.get", func(ctx context.Context, p nameArg) (skills.Skill, error) {
		st, err := d.skillStore()
		if err != nil {
			return skills.Skill{}, err
		}
		sk, err := st.Get(p.Name)
		return sk, skillErr(err)
	})
	handle(d, "skills.save", func(ctx context.Context, p struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}) (skills.Skill, error) {
		return mutate("save", p.Name, func(st *skills.Store) (skills.Skill, error) { return st.Save(p.Name, p.Body, "") })
	})
	handle(d, "skills.delete", func(ctx context.Context, p nameArg) (none, error) {
		st, err := d.skillStore()
		if err != nil {
			return none{}, err
		}
		if err := st.Delete(p.Name); err != nil {
			d.audit("skills.delete", p.Name, "failed", err.Error())
			return none{}, skillErr(err)
		}
		d.audit("skills.delete", p.Name, "allowed", "")
		d.skillsChanged("delete", p.Name)
		return none{}, nil
	})
	handle(d, "skills.setEnabled", func(ctx context.Context, p struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}) (skills.Skill, error) {
		action := "disable"
		if p.Enabled {
			action = "enable"
		}
		return mutate(action, p.Name, func(st *skills.Store) (skills.Skill, error) { return st.SetEnabled(p.Name, p.Enabled) })
	})
	handle(d, "skills.discover", func(ctx context.Context, _ none) ([]skills.Discovered, error) {
		st, err := d.skillStore()
		if err != nil {
			return nil, err
		}
		loc, err := skills.DefaultLocations()
		if err != nil {
			return nil, err
		}
		found, err := skills.Discover(st, loc)
		d.audit("skills.discover", "", "allowed", "")
		return found, skillErr(err)
	})
	handle(d, "skills.import", func(ctx context.Context, p struct {
		Harness string `json:"harness"`
		Name    string `json:"name"`
	}) (skills.Skill, error) {
		loc, err := skills.DefaultLocations()
		if err != nil {
			return skills.Skill{}, err
		}
		return mutate("import", p.Harness+"/"+p.Name, func(st *skills.Store) (skills.Skill, error) {
			return skills.Import(st, loc, p.Harness, p.Name)
		})
	})
	handle(d, "skills.syncState", func(ctx context.Context, _ none) ([]skills.SyncState, error) {
		y, err := d.syncer()
		if err != nil {
			return nil, err
		}
		d.syncMu.Lock()
		defer d.syncMu.Unlock()
		out, err := y.State("")
		return out, skillErr(err)
	})
	handle(d, "skills.sync", func(ctx context.Context, p struct {
		Harness string `json:"harness"`
	}) ([]skills.SyncState, error) {
		y, err := d.syncer()
		if err != nil {
			return nil, err
		}
		d.syncMu.Lock()
		defer d.syncMu.Unlock()
		out, err := y.Sync(p.Harness)
		if err != nil {
			d.audit("skills.sync", p.Harness, "failed", err.Error())
			return nil, skillErr(err)
		}
		detail := ""
		for _, s := range out {
			if s.Error != "" {
				detail += s.Harness + ": " + s.Error + "; "
			}
		}
		outcome := "allowed"
		if detail != "" {
			outcome = "partial"
		}
		d.audit("skills.sync", p.Harness, outcome, detail)
		d.skillsChanged("sync", p.Harness)
		return out, nil
	})

	handle(d, gateway.MethodTools, func(ctx context.Context, _ gateway.ToolsParams) ([]gateway.Tool, error) {
		if d.Gateway == nil {
			return nil, errors.New("gateway is unavailable")
		}
		return d.Gateway.Tools(ctx)
	})
	handle(d, gateway.MethodCall, func(ctx context.Context, p gateway.CallParams) (*mcp.CallResult, error) {
		if d.Gateway == nil {
			return nil, errors.New("gateway is unavailable")
		}
		res, err := d.Gateway.Call(ctx, p.SessionID, p.Name, json.RawMessage(p.Arguments))
		if errors.Is(err, policy.ErrDenied) {
			return nil, &ipc.Error{Code: ipc.CodeDenied, Message: err.Error()}
		}
		return res, err
	})
	handle(d, gateway.MethodSkills, func(ctx context.Context, _ none) ([]skills.Skill, error) {
		if d.Gateway == nil {
			return nil, errors.New("gateway is unavailable")
		}
		list, err := d.Gateway.SkillList()
		return list, skillErr(err)
	})
	handle(d, gateway.MethodSkill, func(ctx context.Context, p gateway.SkillParams) (gateway.SkillContent, error) {
		if d.Gateway == nil {
			return gateway.SkillContent{}, errors.New("gateway is unavailable")
		}
		c, err := d.Gateway.SkillGet(p.Name, p.File)
		if err != nil {
			return gateway.SkillContent{}, skillErr(err)
		}
		d.audit("gateway.skill.get", p.Name+"/"+c.File, "allowed", "")
		return c, nil
	})
}
