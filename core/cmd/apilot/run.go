package main

import (
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{Use: "run", Short: "Inspect and stop runs"}

func init() { rootCmd.AddCommand(runCmd) }
