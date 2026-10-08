package main

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
)

// System describes the host.
type System struct{}

// Info returns host information.
func (System) Info() map[string]string {
	return map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH}
}

func main() {
	mygo.Bind(System{})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Orole OS", Width: 1280, Height: 800, URL: "/", StateKey: "main"})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
