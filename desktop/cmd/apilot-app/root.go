package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/integrations"
	"github.com/alswl/apilot/core/pkg/server"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui"
	"github.com/alswl/apilot/desktop/internal/ui/workbench"
	"github.com/egoist/mygo"
)

func Execute() int {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		return integrations.Serve()
	}
	if mygo.IsDev() && os.Getenv("APILOT_DEV_SCENARIO") != "" {
		name := os.Getenv("APILOT_DEV_SCENARIO")
		log.Printf("development scenario: %s", name)
		if err := workbench.RunDevScenario(name); err != nil {
			log.Print(err)
			return 1
		}
		return 0
	}
	flags := flag.NewFlagSet("apilot-app", flag.ContinueOnError)
	framework := flags.Bool("workbench", false, "open the live workbench")
	mock := flags.Bool("mock", false, "open the workbench with local fixture data")
	capture := flags.String("capture", "", "save workbench screenshots in mock mode")
	keybindings := flags.String("keybindings", "", "load workbench keyboard bindings from this JSON file")
	language := flags.String("language", "", "workbench language: system, en, or zh-Hans")
	serve := flags.Bool("serve", false, "run the local server without a window")
	verify := flags.Bool("verify-lifecycle", false, "run the lifecycle verification app")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	workbenchMode := *mock || *framework
	if flags.NArg() != 0 || (*capture != "" && !*mock) ||
		((*keybindings != "" || *language != "") && (*serve || *verify)) ||
		(*mock && *framework) || (workbenchMode && *verify) || (*serve && (workbenchMode || *verify)) {
		log.Print("usage: apilot-app [--serve | --workbench | --mock [--capture DIR] | --verify-lifecycle] [--keybindings FILE] [--language LANG]")
		return 2
	}
	if *mock {
		if err := workbench.RunMock(*capture, *keybindings, *language); err != nil {
			log.Print(err)
			return 1
		}
		return 0
	}
	dir := config.DataDir()
	remote := services.NewRemote(config.SocketPath(dir))
	var runtime *server.Runtime
	if *serve || *verify {
		var err error
		runtime, err = server.Open(dir)
		if err != nil {
			log.Print(err)
			return 1
		}
	} else if _, err := remote.Call("workbench.health", nil); err != nil {
		if common.ProtocolError(err).Code != "APP_NOT_RUNNING" {
			log.Print(err)
			return 1
		}
		if _, legacyErr := remote.Call("status", nil); legacyErr == nil {
			log.Print("Restart the existing Apilot service to enable the workbench HTTP API")
			return 1
		}
		runtime, err = server.Open(dir)
		if err != nil {
			log.Print(err)
			return 1
		}
	}
	if runtime != nil {
		defer runtime.Close()
	}
	if *serve {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-ctx.Done()
		return 0
	}
	if err := ui.Run(runtime, remote, !*verify, *verify, *keybindings, *language); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
