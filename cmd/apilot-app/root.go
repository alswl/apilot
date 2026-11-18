package main

import (
	"flag"
	"log"
	"os"

	"github.com/alswl/apilot/internal/ui"
	"github.com/alswl/apilot/internal/ui/workbench"
	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/integrations"
	"github.com/alswl/apilot/pkg/server"
)

func Execute() int {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		return integrations.Serve()
	}
	flags := flag.NewFlagSet("apilot-app", flag.ContinueOnError)
	mock := flags.Bool("mock", false, "open the workbench with local fixture data")
	capture := flags.String("capture", "", "save workbench screenshots in mock mode")
	keybindings := flags.String("keybindings", "", "load keyboard bindings from this JSON file in mock mode")
	language := flags.String("language", "system", "workbench language: system, en, or zh-Hans")
	verify := flags.Bool("verify-lifecycle", false, "run the lifecycle verification app")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || ((*capture != "" || *keybindings != "" || *language != "system") && !*mock) || (*mock && *verify) {
		log.Print("usage: apilot-app [--mock [--capture DIR] [--keybindings FILE] [--language LANG] | --verify-lifecycle]")
		return 2
	}
	if *mock {
		if err := workbench.RunMock(*capture, *keybindings, *language); err != nil {
			log.Print(err)
			return 1
		}
		return 0
	}
	runtime, err := server.Open(config.DataDir())
	if err != nil {
		log.Print(err)
		return 1
	}
	defer runtime.Close()
	if err := ui.Run(runtime, *verify); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
