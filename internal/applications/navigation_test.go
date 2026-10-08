package applications

import "testing"

func TestDeepLinksOnlyNavigateBuiltInModules(t *testing.T) {
	for _, view := range []string{"home", "agents", "review", "worktrees", "automations", "mail", "drive", "github", "projects", "terminals", "applications", "browser", "files", "settings"} {
		got, err := ParseDeepLink("orole://open/" + view)
		if err != nil || got != view {
			t.Fatalf("%s: %q, %v", view, got, err)
		}
	}
	for _, raw := range []string{
		"https://open/home", "file:///C:/Windows/notepad.exe", "orole://open/machines", "orole://launch/shortcut-id", "orole://open/applications?id=123", "orole://open/home?", "orole://open/home#", "orole://open/home#run", "orole://user:secret@open/home", "orole://open:80/home", "orole://open/%68ome", "orole://open/../home", "orole://open/home/", "orole://open//home", "orole:open/home", "orole://open/C:/Windows/cmd.exe", "orole://open/Home", "OROLE://open/home", " orole://open/home", "orole://open/home\n", "orole://open/%00home",
	} {
		if view, err := ParseDeepLink(raw); err == nil {
			t.Errorf("accepted %q as %q", raw, view)
		}
	}
}
