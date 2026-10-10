package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/store"
	"log"
)

func main() {
	path, err := store.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	storage, err := store.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	a := newApplication(storage)
	mygo.Theme.SetSource(mygo.ThemeLight)
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Syslog Tester", Width: 1200, Height: 800, MinWidth: 980, MinHeight: 660,
			TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: 52,
			Vibrancy: mygo.VibrancySidebar, Content: ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
