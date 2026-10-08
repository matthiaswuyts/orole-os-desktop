package daemon_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

func TestNotificationTestUsesSavedPreferencesAndEvents(t *testing.T) {
	dir := t.TempDir()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := control.NewClient(dir, "notification-sound-test")
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	events := make(chan daemon.NotificationEvent, 8)
	client.OnEvent(func(name string, data json.RawMessage) {
		if name == "notifications.changed" {
			var event daemon.NotificationEvent
			if json.Unmarshal(data, &event) == nil {
				events <- event
			}
		}
	})
	if err := client.Subscribe(ctx, "notifications.*"); err != nil {
		t.Fatal(err)
	}
	call := func(method string, params, result any) {
		t.Helper()
		if err := client.Call(ctx, method, params, result); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	nextEvent := func() daemon.NotificationEvent {
		t.Helper()
		select {
		case event := <-events:
			return event
		case <-ctx.Done():
			t.Fatal("notification event not received")
			return daemon.NotificationEvent{}
		}
	}
	prefs := notifications.DefaultPrefs()
	prefs.Sources["system.test"] = notifications.SourcePref{Enabled: true, Native: true, Sound: false}
	call("notifications.prefs.set", prefs, nil)
	var result notifications.Result
	call("notifications.test", nil, &result)
	if !result.Stored || !result.Native || result.Sound || result.Notification.Source != "system.test" || result.Notification.ID == "" {
		t.Fatalf("silent native test result: %+v", result)
	}
	event := nextEvent()
	if event.Kind != "created" || !event.Native || event.Sound || event.Notification == nil || event.Notification.ID != result.Notification.ID {
		t.Fatalf("silent native creation event: %+v", event)
	}
	var list daemon.NotificationList
	call("notifications.list", notifications.ListOptions{Source: "system.test"}, &list)
	if len(list.Items) != 1 || list.Items[0].ID != result.Notification.ID {
		t.Fatalf("test was not actually persisted: %+v", list)
	}
	for _, method := range []string{"notifications.markRead", "notifications.dismiss"} {
		call(method, map[string]any{"ids": []string{result.Notification.ID}}, nil)
		event = nextEvent()
		if event.Kind != "updated" || event.Native || event.Sound || event.Notification != nil {
			t.Fatalf("%s must not request audio or a native toast: %+v", method, event)
		}
	}
	prefs.Sources["system.test"] = notifications.SourcePref{Enabled: true, Native: false, Sound: true}
	call("notifications.prefs.set", prefs, nil)
	call("notifications.test", nil, &result)
	event = nextEvent()
	if !result.Stored || result.Native || !result.Sound || event.Kind != "created" || event.Native || !event.Sound {
		t.Fatalf("in-app audible test: result=%+v event=%+v", result, event)
	}
	prefs.Sources["system.test"] = notifications.SourcePref{Enabled: false, Native: true, Sound: true}
	call("notifications.prefs.set", prefs, nil)
	call("notifications.test", nil, &result)
	if result.Stored || result.Native || result.Sound {
		t.Fatalf("disabled source requested delivery: %+v", result)
	}
	call("notifications.list", notifications.ListOptions{Source: "system.test", IncludeDismissed: true}, &list)
	if len(list.Items) != 2 {
		t.Fatalf("disabled test must not add history: %+v", list)
	}
}
