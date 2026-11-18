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
	framework := flags.Bool("workbench", false, "open the workbench with no source data")
	mock := flags.Bool("mock", false, "open the workbench with local fixture data")
	capture := flags.String("capture", "", "save workbench screenshots in mock mode")
	keybindings := flags.String("keybindings", "", "load workbench keyboard bindings from this JSON file")
	language := flags.String("language", "system", "workbench language: system, en, or zh-Hans")
	verify := flags.Bool("verify-lifecycle", false, "run the lifecycle verification app")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	workbenchMode := *mock || *framework
	if flags.NArg() != 0 || (*capture != "" && !*mock) ||
		((*keybindings != "" || *language != "system") && !workbenchMode) ||
		(*mock && *framework) || (workbenchMode && *verify) {
		log.Print("usage: apilot-app [--workbench | --mock [--capture DIR] | --verify-lifecycle] [--keybindings FILE] [--language LANG]")
		return 2
	}
	if workbenchMode {
		var err error
		if *framework {
			err = workbench.RunFramework(*keybindings, *language)
		} else {
			err = workbench.RunMock(*capture, *keybindings, *language)
		}
		if err != nil {
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
