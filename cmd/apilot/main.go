package main

import (
	"github.com/alswl/apilot/internal/cli"
	"os"
)

func main() { os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr)) }
