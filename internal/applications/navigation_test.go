package applications

import "testing"

func TestDeepLinksOnlyNavigateBuiltInModules(t *testing.T) {
	for _, view := range []string{"home", "agents", "review", "automations", "skills", "mcp", "settings"} {
		got, err := ParseDeepLink("orole://open/" + view)
		if err != nil || got != view {
			t.Fatalf("%s: %q, %v", view, got, err)
		}
	}
	for _, raw := range []string{
		"orole://open/images",
		"orole://open/worktrees", "orole://open/mail", "orole://open/drive", "orole://open/github", "orole://open/projects", "orole://open/terminals",
		"orole://open/applications", "orole://open/browser", "orole://open/files", "orole://open/machines",
		"https://open/home", "file:///C:/Windows/notepad.exe", "orole://launch/shortcut-id", "orole://open/applications?id=123", "orole://open/home?", "orole://open/home#", "orole://open/home#run", "orole://user:secret@open/home", "orole://open:80/home", "orole://open/%68ome", "orole://open/../home", "orole://open/home/", "orole://open//home", "orole:open/home", "orole://open/C:/Windows/cmd.exe", "orole://open/Home", "OROLE://open/home", " orole://open/home", "orole://open/home\n", "orole://open/%00home",
	} {
		if view, err := ParseDeepLink(raw); err == nil {
			t.Errorf("accepted %q as %q", raw, view)
		}
	}
}
