package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

// botDraftActor is the bot builder's preview conversation: it is not a saved bot and never gets
// more than the policy defaults.
const botDraftActor = chat.BotPrefix + "draft"

// botLookup reads one active bot (chat.Manager satisfies it).
type botLookup interface {
	Bot(ctx context.Context, id string) (chat.Bot, error)
}

// toolAccessLimit maps a chat.ToolAccess* level to the highest effect it runs without asking.
// Unknown levels and "ask" grant nothing.
func toolAccessLimit(level string) policy.Effect {
	switch level {
	case chat.ToolAccessRead:
		return policy.EffectRead
	case chat.ToolAccessWrite:
		return policy.EffectWrite
	case chat.ToolAccessAll:
		return policy.EffectDestructive
	}
	return policy.EffectUnknown
}

func validToolAccess(level string) bool {
	switch level {
	case chat.ToolAccessAsk, chat.ToolAccessRead, chat.ToolAccessWrite, chat.ToolAccessAll:
		return true
	}
	return false
}

// globalBotToolAccess is the "agents" setting's botToolAccess; unset, unknown or unreadable means ask.
func (d *Daemon) globalBotToolAccess() string {
	v, ok := d.agentSettings()
	if !ok || !validToolAccess(v.BotToolAccess) {
		return chat.ToolAccessAsk
	}
	return v.BotToolAccess
}

// botToolAccess resolves a bot actor's effective tool access level and the setting it comes from:
// the bot's own ToolAccess when set, else the global setting. Anything that is not a saved, active
// bot (users, agents, the builder draft, unknown or archived bots, lookup failures) and any unknown
// level is "ask".
func (d *Daemon) botToolAccess(ctx context.Context, bots botLookup, actor string) (level, setting string) {
	id, ok := strings.CutPrefix(actor, chat.BotPrefix)
	if !ok || id == "" || actor == botDraftActor || bots == nil {
		return chat.ToolAccessAsk, ""
	}
	b, err := bots.Bot(ctx, id)
	if err != nil {
		return chat.ToolAccessAsk, ""
	}
	if lvl := b.Config.ToolAccess; lvl != "" {
		if !validToolAccess(lvl) {
			return chat.ToolAccessAsk, ""
		}
		return lvl, "this bot's tool access setting"
	}
	return d.globalBotToolAccess(), "the global bot tool access setting"
}

// botAutoApproveHook returns the policy engine's AutoApprove hook: bots run actions up to their tool
// access level without an approval prompt. The engine applies it only to default "ask" decisions,
// so per-tool permissions and stored rules still win.
func (d *Daemon) botAutoApproveHook(bots botLookup) policy.AutoApprove {
	return func(ctx context.Context, actor string) (policy.Effect, string) {
		level, setting := d.botToolAccess(ctx, bots, actor)
		limit := toolAccessLimit(level)
		if limit == policy.EffectUnknown {
			return limit, ""
		}
		return limit, fmt.Sprintf("allowed without asking by %s (%q)", setting, level)
	}
}
