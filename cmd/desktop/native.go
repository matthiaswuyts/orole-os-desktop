package main

import (
	"log"
	"os"
	"sync"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/app"
	"github.com/rayorole/orole-os-desktop/internal/applications"
)

const launcherShortcut = "Ctrl+Alt+Space"

// nativeDesktop owns only GUI-lifetime state. Call prepareNativeDesktop before
// App.Run and Attach in WhenReady after creating the main window.
type nativeDesktop struct {
	mu       sync.Mutex
	window   *mygo.Window
	tray     *mygo.Tray
	menu     *mygo.Menu
	quitting bool
	pending  app.DesktopNavigation
}

func prepareNativeDesktop() (*nativeDesktop, bool) {
	n := &nativeDesktop{}
	// Generating bindings must not register a temporary executable as a URL handler.
	if os.Getenv("MYGO_GENERATE") != "" {
		return n, true
	}
	mygo.App.OnOpenURL(func(raw string) {
		view, err := applications.ParseDeepLink(raw)
		if err != nil {
			// Never log the rejected URL: it may carry untrusted credentials/content.
			log.Print("desktop: rejected unsupported deep link")
			return
		}
		n.navigate(view)
	})
	mygo.App.OnSecondInstance(func(_ []string, _ string) { n.show() })
	mygo.App.OnActivate(func(_ bool) { n.show() })
	mygo.App.OnBeforeQuit(func(_ *mygo.QuitEvent) {
		n.mu.Lock()
		n.quitting = true
		n.mu.Unlock()
	})
	mygo.App.OnWillQuit(func(_ *mygo.QuitEvent) {
		// Release only our shortcut while the native event loop is still alive.
		mygo.GlobalShortcut.Unregister(launcherShortcut)
		n.mu.Lock()
		tray := n.tray
		n.tray = nil
		n.mu.Unlock()
		if tray != nil {
			tray.Destroy()
		}
	})
	// MyGo uses its per-user local Unix socket (also on Windows), not a TCP listener.
	// Register listeners first so forwarded launch arguments cannot be missed.
	if !mygo.App.RequestSingleInstanceLock() {
		return n, false
	}
	// Development runs must not replace the installed app's OS scheme association.
	// mygo dev still delivers explicit launch arguments from urlSchemes metadata.
	if !mygo.IsDev() {
		if err := mygo.App.RegisterURLScheme("orole"); err != nil {
			log.Printf("desktop: could not register orole URL scheme: %v", err)
		}
	}
	n.menu = mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Orole OS", Submenu: []*mygo.MenuItem{
			{Label: "Show Orole OS", Click: func(*mygo.MenuItem, *mygo.Window) { n.show() }},
			{ID: "launcher", Label: "Open Home", Click: func(*mygo.MenuItem, *mygo.Window) { n.navigate("home") }},
			{ID: "hide", Label: "Hide to tray", Accelerator: "CmdOrCtrl+Shift+H", Disabled: true, Click: func(*mygo.MenuItem, *mygo.Window) { n.hide() }},
			mygo.Separator(),
			{Label: "Quit Orole OS", Accelerator: "CmdOrCtrl+Q", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "Navigate", Submenu: []*mygo.MenuItem{
			{Label: "Home", Accelerator: "CmdOrCtrl+Shift+1", Click: func(*mygo.MenuItem, *mygo.Window) { n.navigate("home") }},
			{Label: "Applications", Accelerator: "CmdOrCtrl+Shift+L", Click: func(*mygo.MenuItem, *mygo.Window) { n.navigate("applications") }},
			{Label: "Settings", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { n.navigate("settings") }},
		}},
	})
	mygo.App.SetMenu(n.menu)
	return n, true
}

func (n *nativeDesktop) Attach(win *mygo.Window) {
	n.mu.Lock()
	n.window = win
	n.mu.Unlock()
	// Keep the menu available via Alt/F10 without changing the Shipyard title bar.
	win.SetAutoHideMenuBar(true)
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon: windowIcon, ToolTip: "Orole OS",
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Show Orole OS", Click: func(*mygo.MenuItem, *mygo.Window) { n.show() }},
			{Label: "Hide Orole OS", Click: func(*mygo.MenuItem, *mygo.Window) { n.hide() }},
			{Label: "Applications", Click: func(*mygo.MenuItem, *mygo.Window) { n.navigate("applications") }},
			mygo.Separator(),
			{Label: "Quit Orole OS", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		}),
	})
	if err != nil {
		// Do not trap the user in an invisible app when tray creation is unavailable.
		log.Printf("desktop: tray unavailable; closing the window will quit: %v", err)
	} else {
		n.mu.Lock()
		n.tray = tray
		n.mu.Unlock()
		n.menu.ItemByID("hide").SetEnabled(true)
	}
	win.OnClose(func(e *mygo.CloseEvent) {
		n.mu.Lock()
		hide := n.tray != nil && !n.quitting
		n.mu.Unlock()
		if hide {
			e.PreventDefault()
			win.Hide()
		}
	})
	win.OnClosed(func() {
		n.mu.Lock()
		n.window = nil
		n.mu.Unlock()
	})
	// Attach runs from WhenReady: global shortcuts require the native event loop.
	// Do not register Ctrl+K globally; it belongs to the focused application.
	if err := mygo.GlobalShortcut.Register(launcherShortcut, func() { n.navigate("home") }); err != nil {
		log.Printf("desktop: system-wide shortcut %s unavailable; use the Orole OS menu or tray: %v", launcherShortcut, err)
		n.menu.ItemByID("launcher").SetLabel("Open Home (system-wide shortcut unavailable)")
	} else {
		// This is a status label, not a second application-menu accelerator.
		n.menu.ItemByID("launcher").SetLabel("Open Home (" + launcherShortcut + " system-wide)")
	}
}

func (n *nativeDesktop) show() {
	n.mu.Lock()
	win := n.window
	n.mu.Unlock()
	if win != nil {
		win.Show()
		if win.IsMinimized() {
			win.Restore()
		}
		win.Focus()
	}
}

func (n *nativeDesktop) hide() {
	n.mu.Lock()
	win, available := n.window, n.tray != nil
	n.mu.Unlock()
	if win != nil && available {
		win.Hide()
	}
}

func (n *nativeDesktop) navigate(view string) {
	if !applications.InternalView(view) {
		return
	}
	n.mu.Lock()
	n.pending.Sequence++
	n.pending.View = view
	request := n.pending
	n.mu.Unlock()
	n.show()
	app.DesktopNavigate.Broadcast(request)
}

// Navigation supplies the latest request when React mounts. Subscribing before
// this snapshot and comparing Sequence handles cold-start and reload races.
func (n *nativeDesktop) Navigation() app.DesktopNavigation {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.pending
}
