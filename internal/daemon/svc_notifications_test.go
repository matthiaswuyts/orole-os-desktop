package daemon_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

type notificationClient struct {
	t      *testing.T
	ctx    context.Context
	client *ipc.Client
	events chan daemon.NotificationEvent
}

func connectNotifications(t *testing.T) *notificationClient {
	t.Helper()
	dir := t.TempDir()
	stop := start(t, dir)
	t.Cleanup(stop)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client := control.NewClient(dir, "notification-test")
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	c := &notificationClient{t: t, ctx: ctx, client: client, events: make(chan daemon.NotificationEvent, 32)}
	client.OnEvent(func(name string, data json.RawMessage) {
		if name == "notifications.changed" {
			var event daemon.NotificationEvent
			if json.Unmarshal(data, &event) == nil {
				c.events <- event
			}
		}
	})
	if err := client.Subscribe(ctx, "notifications.*"); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *notificationClient) call(method string, params, result any) {
	c.t.Helper()
	if err := c.client.Call(c.ctx, method, params, result); err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
}

func (c *notificationClient) next() daemon.NotificationEvent {
	c.t.Helper()
	select {
	case event := <-c.events:
		return event
	case <-c.ctx.Done():
		c.t.Fatal("notification event not received")
		return daemon.NotificationEvent{}
	}
}

func TestNotificationPreferencesPerKind(t *testing.T) {
	c := connectNotifications(t)
	prefs := notifications.DefaultPrefs()
	prefs.Kinds[notifications.Message] = notifications.KindPref{InApp: true, Sound: false}
	c.call("notifications.prefs.set", prefs, nil)
	var result notifications.Result
	c.call("notifications.test", nil, &result)
	if !result.Stored || result.Sound || result.Notification.Kind != notifications.Message || result.Notification.ID == "" {
		t.Fatalf("silent test result: %+v", result)
	}
	event := c.next()
	if event.Kind != "created" || event.Sound || event.Unread != 1 || event.Notification == nil || event.Notification.ID != result.Notification.ID {
		t.Fatalf("silent creation event: %+v", event)
	}
	c.call("notifications.markRead", map[string]any{"ids": []string{result.Notification.ID}}, nil)
	if event = c.next(); event.Kind != "updated" || event.Sound || event.Unread != 0 || event.Notification != nil {
		t.Fatalf("read must only update counts: %+v", event)
	}
	prefs.Kinds[notifications.Message] = notifications.KindPref{InApp: true, Sound: true}
	c.call("notifications.prefs.set", prefs, nil)
	c.call("notifications.test", nil, &result)
	if event = c.next(); !result.Stored || !result.Sound || event.Kind != "created" || !event.Sound {
		t.Fatalf("audible test: result=%+v event=%+v", result, event)
	}
	prefs.Kinds[notifications.Message] = notifications.KindPref{InApp: false, Sound: true}
	c.call("notifications.prefs.set", prefs, nil)
	c.call("notifications.test", nil, &result)
	if result.Stored || result.Sound {
		t.Fatalf("a kind turned off was delivered: %+v", result)
	}
	var list daemon.NotificationList
	c.call("notifications.list", notifications.ListOptions{}, &list)
	if len(list.Items) != 2 || list.Unread != 1 {
		t.Fatalf("history: %+v", list)
	}
}

// A flow run reports its approval as "Needs you" until the user answers, then its own
// Notify step and, because the user started it, its finish.
func TestFlowRunNotifications(t *testing.T) {
	c := connectNotifications(t)
	definition := automations.Definition{Name: "Ship release", Steps: []automations.Step{
		{ID: "gate", Name: "Gate", Type: "approval", Params: json.RawMessage(`{"message":"Publish the release?"}`)},
		{ID: "note", Name: "Note", Type: "notify", Params: json.RawMessage(`{"title":"Released","body":"v1.2","level":"success"}`)},
	}}
	var saved automations.SaveResult
	c.call("automations.save", definition, &saved)
	if !saved.Saved {
		t.Fatalf("save: %+v", saved.Issues)
	}
	var run automations.Run
	c.call("automations.run", map[string]string{"id": saved.Definition.ID}, &run)
	target := "automation:" + saved.Definition.ID

	event := c.next()
	n := event.Notification
	if event.Kind != "created" || n == nil || n.Kind != notifications.Approval || !n.NeedsYou || event.NeedsYou != 1 || n.Target != target || n.Title != "Ship release" || n.Body != "Publish the release?" {
		t.Fatalf("approval: %+v %+v", event, n)
	}
	var waiting daemon.NotificationList
	c.call("notifications.list", notifications.ListOptions{NeedsYou: true}, &waiting)
	if len(waiting.Items) != 1 || waiting.Items[0].ID != n.ID {
		t.Fatalf("needs-you list: %+v", waiting)
	}

	c.call("automations.approve", map[string]any{"runId": run.ID, "stepId": "gate", "approved": true}, nil)
	var kinds []notifications.Kind
	for len(kinds) < 2 {
		event = c.next()
		switch {
		case event.Kind == "updated":
			if event.NeedsYou != 0 {
				t.Fatalf("approval still waiting after the answer: %+v", event)
			}
		case event.Notification != nil:
			if event.Notification.Target != target {
				t.Fatalf("target: %+v", event.Notification)
			}
			kinds = append(kinds, event.Notification.Kind)
		}
	}
	if kinds[0] != notifications.Message || kinds[1] != notifications.Finished {
		t.Fatalf("after approval: %v", kinds)
	}
	c.call("notifications.list", notifications.ListOptions{NeedsYou: true}, &waiting)
	if len(waiting.Items) != 0 || waiting.NeedsYou != 0 {
		t.Fatalf("resolved approval still listed: %+v", waiting)
	}
}
