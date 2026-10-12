package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/events"
)

// Orole permission modes. Manual, Auto and YOLO are what the UI offers; Plan is a read-only
// mode only the daemon itself uses (reviewer and image sessions). Each adapter maps a mode
// onto its harness's own approval controls; the empty mode means Manual.
const (
	// PermManual asks before anything the harness does not know to be safe.
	PermManual = "manual"
	// PermAuto lets the harness's own AI decide which actions are safe to run unasked, or, for a
	// harness without one, runs the closest sandboxed equivalent (see PermissionModeSupport).
	PermAuto = "auto"
	// PermYolo never asks. Orole's MCP gateway policy still applies to its own tools.
	PermYolo = "yolo"
	// PermPlan is read-only; it is not user selectable.
	PermPlan = "plan"
)

// ErrRestartRequired means a harness cannot switch permission mode on a live conversation.
var ErrRestartRequired = errors.New("this agent can only change permission mode by restarting its conversation")

func normalizePermissionMode(m string) string {
	if m == "" {
		return PermManual
	}
	return m
}

func validPermissionMode(m string) error {
	switch m {
	case "", PermManual, PermAuto, PermYolo, PermPlan:
		return nil
	}
	return fmt.Errorf("unsupported permission mode %q", m)
}

// PermissionModeSupport tells the UI how one harness realises one Orole mode, so a tooltip never
// claims more than the harness does.
type PermissionModeSupport struct {
	Mode string `json:"mode"` // manual | auto | yolo
	// Available is false when the mode cannot be used at all; Detail then says why.
	Available bool `json:"available"`
	// Approximated is true when the harness has no native equivalent and Orole runs the closest
	// sandboxed mode instead (Auto only).
	Approximated bool   `json:"approximated"`
	Detail       string `json:"detail"`
}

// permissionModeReporter is implemented by adapters whose modes depend on the installed CLI.
type permissionModeReporter interface {
	PermissionModes(ctx context.Context, d Detection) []PermissionModeSupport
}

// permissionModeProcess is a live process that can change its permission mode in place.
// nextTurn reports that the mode takes effect from the next turn rather than immediately.
type permissionModeProcess interface {
	SetPermissionMode(mode string) (nextTurn bool, err error)
}

func modeSupport(mode string, approximated bool, detail string) PermissionModeSupport {
	return PermissionModeSupport{Mode: mode, Available: true, Approximated: approximated, Detail: detail}
}

// yoloUnavailable marks YOLO unusable in every list, for the daemon setting that disables it.
func yoloUnavailable(in []PermissionModeSupport) []PermissionModeSupport {
	out := slices.Clone(in)
	for i := range out {
		if out[i].Mode == PermYolo {
			out[i].Available, out[i].Detail = false, "YOLO mode is turned off in Orole's settings."
		}
	}
	return out
}

// remotePermissionModes lists the modes a remote one-shot process can honour.
func remotePermissionModes(kind, help string) []PermissionModeSupport {
	off := func(mode, why string) PermissionModeSupport { return PermissionModeSupport{Mode: mode, Detail: why} }
	manual := modeSupport(PermManual, false, "Prompts cannot be answered in a remote one-shot run, so the harness's own default denies what is not known safe.")
	if kind != "claude" {
		const why = "Remote Codex runs `exec` in its workspace sandbox with no approval prompts; permission modes do not apply."
		return []PermissionModeSupport{manual, off(PermAuto, why), off(PermYolo, why)}
	}
	auto := off(PermAuto, "This Claude Code does not advertise auto mode.")
	if strings.Contains(help, `"auto"`) {
		auto = modeSupport(PermAuto, false, "Claude Code's classifier reviews each action.")
	}
	return []PermissionModeSupport{manual, auto, off(PermYolo, "YOLO is not available on remote machines.")}
}

// permissionModesApply rejects modes a launch shape cannot honour, so a session never claims a
// mode its harness is ignoring. Terminal Claude passes the mode to the TUI; other terminal CLIs
// keep their own approvals.
func permissionModesApply(kind string, mode Mode, remote bool, perm string) error {
	perm = normalizePermissionMode(perm)
	if perm == PermManual {
		return nil
	}
	switch {
	case remote && perm == PermYolo:
		return errors.New("YOLO mode is not available on remote machines")
	case remote && kind != "claude":
		return fmt.Errorf("remote %s runs without approval prompts; only Manual is available", kind)
	case !remote && mode == ModeTerminal && kind != "claude":
		return fmt.Errorf("%s terminal sessions use the CLI's own approvals; Orole permission modes apply to structured sessions", kind)
	}
	return nil
}

// withYolo returns detections with YOLO marked unavailable while the setting is off; the cached
// slice is never modified.
func (m *Manager) withYolo(in []Detection) []Detection {
	if m.yoloEnabled() {
		return in
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].PermissionModes = yoloUnavailable(out[i].PermissionModes)
	}
	return out
}

// yoloEnabled reports whether the daemon setting allows YOLO sessions.
func (m *Manager) yoloEnabled() bool {
	return m.cfg.YoloEnabled == nil || m.cfg.YoloEnabled()
}

// gateYolo enforces, in Go, that YOLO is explicit: switched on in settings, confirmed in the
// request, and never started by an automation. Every attempt is audited.
func (m *Manager) gateYolo(ctx context.Context, target, perm string, confirmed bool) error {
	if perm != PermYolo {
		return nil
	}
	deny := func(err error) error {
		m.cfg.Audit("agent.yolo", target, "denied", err.Error())
		return err
	}
	switch {
	case !m.yoloEnabled():
		return deny(errors.New("YOLO mode is turned off in Orole's settings"))
	case len(events.ChainFrom(ctx)) > 0:
		return deny(errors.New("automations cannot start or switch to YOLO mode"))
	case ActorFrom(ctx) != "":
		return deny(errors.New("bots cannot start or switch to YOLO mode"))
	case !confirmed:
		return deny(errors.New("YOLO mode needs explicit confirmation: no approval prompts will be shown (confirmYolo)"))
	}
	return nil
}

// permissionControl describes whether a session's mode can be changed now and how.
func (m *Manager) permissionControl(s Session, ls *liveSess) (ok, restarts bool, reason string) {
	switch {
	case !localMachine(s.Machine):
		return false, false, "Remote one-shot runs cannot change permission mode; start a new task."
	case s.Mode == ModeTerminal:
		return false, false, "Terminal sessions use the CLI's own permission controls; change it in the terminal."
	case s.Mode != ModeStructured:
		return false, false, "This session cannot change permission mode."
	case s.PermissionMode == PermPlan:
		return false, false, "Read-only review sessions keep their mode."
	case ls == nil:
		return true, false, ""
	}
	if _, live := ls.proc.(permissionModeProcess); live {
		return true, false, ""
	}
	return true, true, ""
}

// ---- Claude Code capability probe ----

type helpProbe struct {
	size int64
	mod  time.Time
	help string
}

var claudeHelpCache sync.Map // executable path -> helpProbe

// claudeHelp is this Claude Code build's --help text, cached per executable build so
// capability checks cost one probe until the CLI updates.
func claudeHelp(ctx context.Context, path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if v, ok := claudeHelpCache.Load(path); ok {
		if p := v.(helpProbe); p.size == fi.Size() && p.mod.Equal(fi.ModTime()) {
			return p.help
		}
	}
	help := localHelp(ctx, path)
	if help != "" {
		claudeHelpCache.Store(path, helpProbe{fi.Size(), fi.ModTime(), help})
	}
	return help
}

// claudeSupportsAuto reports whether this Claude Code build lists `auto` among its
// --permission-mode choices.
func claudeSupportsAuto(ctx context.Context, path string) bool {
	return strings.Contains(claudeHelp(ctx, path), `"auto"`)
}

// ---- Manager.SetPermissionMode ----

// SetPermissionMode changes a session's permission mode. A live Claude Code conversation
// switches immediately, Codex from its next turn, and Oh My Pi by resuming its conversation
// (only while idle). An ended session just records the mode for its next launch. Terminal and
// remote sessions refuse.
func (m *Manager) SetPermissionMode(ctx context.Context, id, mode string, confirmYolo bool) (Session, error) {
	switch mode {
	case PermManual, PermAuto, PermYolo:
	default:
		return Session{}, fmt.Errorf("permission mode must be manual, auto or yolo, not %q", mode)
	}
	if err := m.gateYolo(ctx, id, mode, confirmYolo); err != nil {
		return Session{}, err
	}
	if err := m.authorize(ctx, "agent.permissionMode", id, "Change this agent's permission mode to "+mode+"."); err != nil {
		return Session{}, err
	}
	s, restart, err := m.applyPermissionMode(ctx, id, mode)
	if err != nil || !restart {
		return s, err
	}
	// Restart-with-resume runs outside the control locks: Stop and relaunch take them.
	if _, err := m.Stop(ctx, id); err != nil {
		return Session{}, err
	}
	return m.relaunch(ctx, id, "", false)
}

func (m *Manager) applyPermissionMode(ctx context.Context, id, mode string) (Session, bool, error) {
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	ls := m.controlSession(id)
	if ls != nil {
		ls.controlMu.Lock()
		defer ls.controlMu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return Session{}, false, err
	}
	s, err := m.Get(id)
	if err != nil {
		return Session{}, false, err
	}
	if ok, _, reason := m.permissionControl(s, ls); !ok {
		return Session{}, false, errors.New(reason)
	}
	if s.PermissionMode == mode {
		return s, false, nil
	}
	from := s.PermissionMode
	restart := false
	if ls != nil {
		m.mu.Lock()
		stopping := m.closing || m.live[id] != ls || ls.stopping
		m.mu.Unlock()
		if stopping || ls.deliveryUncertain {
			return Session{}, false, errors.New("agent is stopping or its previous delivery is uncertain")
		}
		restart = true
		if pp, ok := ls.proc.(permissionModeProcess); ok {
			_, err := pp.SetPermissionMode(mode)
			switch {
			case err == nil:
				restart = false
			case !errors.Is(err, ErrRestartRequired):
				return Session{}, false, err
			}
		}
		if restart {
			if s.Status == StatusWorking || s.Status == StatusNeedsYou || len(s.Pending) > 0 || len(s.Questions) > 0 {
				return Session{}, false, errors.New("this agent applies a new permission mode by restarting its conversation; wait for the current turn to finish")
			}
			if s.AgentSessionID == "" || !m.adapterCanResume(s.Kind) {
				return Session{}, false, errors.New("this agent has no resumable conversation to restart with the new permission mode")
			}
			// The conversation resumes to apply the new mode; the mode picker already shows it.
		}
	}
	s, err = m.mutate(id, func(s *Session) { s.PermissionMode = mode })
	if err != nil {
		return s, false, err
	}
	// No transcript notice: the picker shows the mode, and the change is audited below.
	m.cfg.Audit("agent.permissionMode", id, "allowed", from+" -> "+mode)
	if mode == PermYolo {
		m.cfg.Audit("agent.yolo", id, "allowed", "switched from "+from)
	}
	return s, restart, nil
}
