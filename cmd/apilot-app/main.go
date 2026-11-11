package main

import (
	"github.com/alswl/apilot/internal/runner"
	"github.com/alswl/apilot/internal/ui"
	"log"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		os.Exit(runner.Serve())
	}
	if err := ui.Run(len(os.Args) > 1 && os.Args[1] == "--verify-lifecycle"); err != nil {
		log.Fatal(err)
	}
}
