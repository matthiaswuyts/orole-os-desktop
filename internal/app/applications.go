package app

import (
	"context"
	"encoding/json"

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
