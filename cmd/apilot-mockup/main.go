//go:build darwin && apilot_mockup

package main

import (
	"flag"
	"log"
	"os"

	"github.com/alswl/apilot/internal/ui/workbench"
)

func main() { os.Exit(Execute()) }

func Execute() int {
	capture := flag.String("capture", "", "save workbench screenshots and quit")
	flag.Parse()
	if err := workbench.RunMock(*capture, "", "system"); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
