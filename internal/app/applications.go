package app

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"sync"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/applications"
)

type ApplicationRef struct {
	ID string `json:"id"`
}

var ApplicationsChanged = mygo.NewEvent[ApplicationRef]("applications:changed")

func forwardApplications(event string, data json.RawMessage) bool {
	if event != "applications.changed" {
		return false
	}
	var ref ApplicationRef
	if json.Unmarshal(data, &ref) == nil {
		ApplicationsChanged.Broadcast(ref)
	}
	return true
}

type Applications struct{ L *Link }

func (a Applications) List(ctx context.Context) ([]applications.Shortcut, error) {
	var out []applications.Shortcut
	err := a.L.call(ctx, "applications.list", struct{}{}, &out)
	return out, err
}
func (a Applications) Save(ctx context.Context, req applications.SaveRequest) (applications.Shortcut, error) {
	var out applications.Shortcut
	err := a.L.call(ctx, "applications.save", req, &out)
	return out, err
}
func (a Applications) Remove(ctx context.Context, id string) error {
	return a.L.call(ctx, "applications.remove", map[string]string{"id": id}, nil)
}
func (a Applications) Launch(ctx context.Context, id string) (applications.LaunchResult, error) {
	var out applications.LaunchResult
	err := a.L.call(ctx, "applications.launch", map[string]string{"id": id}, &out)
	return out, err
}

func (Applications) PickExecutable(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: "Choose a native application", ButtonLabel: "Choose application",
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// DesktopNavigation is an ephemeral shell navigation request, never an external URL.
// Sequence lets React subscribe before fetching the last request without replay races.
type DesktopNavigation struct {
	View     string `json:"view"`
	Sequence uint64 `json:"sequence"`
}

var DesktopNavigate = mygo.NewEvent[DesktopNavigation]("desktop:navigate")

// Desktop forwards to the native lifecycle controller. It owns no durable state.
type Desktop struct{ PendingNavigation func() DesktopNavigation }

func (d Desktop) Ready() DesktopNavigation {
	if d.PendingNavigation == nil {
		return DesktopNavigation{}
	}
	return d.PendingNavigation()
}

// SettingsWindowSection asks an already-open settings window to show a section.
var SettingsWindowSection = mygo.NewEvent[string]("settings-window:section")

var (
	settingsMu     sync.Mutex
	settingsWindow *mygo.Window
	sectionID      = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)
)

// OpenSettings opens the isolated settings window, or focuses it and switches to
// section when it is already open. An empty or unknown section shows the first.
func (Desktop) OpenSettings(section string) error {
	if !sectionID.MatchString(section) {
		section = ""
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if win := settingsWindow; win != nil {
		win.Show()
		if win.IsMinimized() {
			win.Restore()
		}
		win.Focus()
		SettingsWindowSection.Broadcast(section)
		return nil
	}
	q := url.Values{"window": {"settings"}}
	if section != "" {
		q.Set("section", section)
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "Orole OS — Settings", Width: 1040, Height: 720, MinWidth: 720, MinHeight: 480,
		BackgroundColor: "#070b14", StateKey: "settings", URL: "/?" + q.Encode(),
		TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 40,
	})
	settingsWindow = win
	win.OnClosed(func() {
		settingsMu.Lock()
		defer settingsMu.Unlock()
		if settingsWindow == win {
			settingsWindow = nil
		}
	})
	return nil
}
