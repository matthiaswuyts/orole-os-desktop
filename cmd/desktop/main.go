// Command orole-os is the Orole OS desktop application (MyGo + React).
package main

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/app"
	"github.com/rayorole/orole-os-desktop/internal/paths"
)

func main() {
	dir, err := paths.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	link := app.NewLink(dir)
	if res, err := mygo.App.Path(mygo.PathResources); err == nil {
		link.DaemonExe = filepath.Join(res, "orole-daemon.exe")
	}
	mygo.Bind(app.Daemon{L: link})
	mygo.Bind(app.Settings{L: link})
	mygo.Bind(app.Workspaces{L: link})
	mygo.Bind(app.Terminals{L: link})

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title: "Orole OS", Width: 1360, Height: 860, MinWidth: 960, MinHeight: 600,
			BackgroundColor: "#070b14", StateKey: "main", URL: "/",
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 40,
		})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := link.Start(ctx); err != nil {
				log.Printf("daemon link: %v", err)
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
