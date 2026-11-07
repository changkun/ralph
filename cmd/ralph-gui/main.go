// Command ralph-gui is a desktop app for running Ralph: choose a project
// folder, set the agent, model, and prompts of each role, start a session
// in a container, and watch what each agent does.
//
// Usage:
//
//	ralph-gui [folder]
//
// Sessions run in a background process, so they keep running when the app
// quits; reopen the folder to follow them again.
package main

import (
	"fmt"
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/unit"

	"github.com/changkun/ralph/internal/session"
)

func main() {
	// The app starts sessions by running itself as "ralph-gui engine
	// <folder>"; see session.Launch.
	if len(os.Args) == 3 && os.Args[1] == "engine" {
		if err := session.Engine(os.Args[2], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 2 || len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Fprintln(os.Stderr, "Usage: ralph-gui [folder]")
		os.Exit(2)
	}
	var folder string
	if len(os.Args) == 2 {
		folder = os.Args[1]
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("Ralph"), app.Size(unit.Dp(1360), unit.Dp(860)), app.MinSize(unit.Dp(960), unit.Dp(560)))
		if err := newUI(w, folder).run(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
