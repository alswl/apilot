package main

import (
	"log"
	"os"

	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/host"
	"github.com/alswl/apilot/internal/runner"
	"github.com/alswl/apilot/internal/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		os.Exit(runner.Serve())
	}
	runtime, err := host.Open(app.DataDir())
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Close()
	if err := ui.Run(runtime, len(os.Args) > 1 && os.Args[1] == "--verify-lifecycle"); err != nil {
		log.Fatal(err)
	}
}
