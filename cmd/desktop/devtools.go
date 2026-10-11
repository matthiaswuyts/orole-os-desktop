package main

import (
	"os"

	"github.com/egoist/mygo"
)

// devTools decides whether the web inspector (and with it Ctrl+Shift+I and F12) exists in the app window. It is off
// for users, in development builds too; a developer turns it on with OROLE_DEVTOOLS=1.
func devTools() mygo.DevTools {
	if os.Getenv("OROLE_DEVTOOLS") == "1" {
		return mygo.DevToolsEnabled
	}
	return mygo.DevToolsDisabled
}
