package main

import (
	"log"
	"os"

	"github.com/alswl/apilot/core/pkg/server"
)

func main() {
	data, err := server.WorkbenchOpenAPI()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		log.Fatal(err)
	}
}
