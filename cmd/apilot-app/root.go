package main

import (
	"log"
	"os"

	"github.com/alswl/apilot/internal/ui"
	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/integrations"
	"github.com/alswl/apilot/pkg/server"
)

func Execute() int {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		return integrations.Serve()
	}
	runtime, err := server.Open(config.DataDir())
	if err != nil {
		log.Print(err)
		return 1
	}
	defer runtime.Close()
	if err := ui.Run(runtime, len(os.Args) > 1 && os.Args[1] == "--verify-lifecycle"); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
