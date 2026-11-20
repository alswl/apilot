package main

import (
	"github.com/spf13/cobra"
)

var observeCmd = &cobra.Command{Use: "observe", Short: "Read external tool state by polling explicit commands"}

func init() { rootCmd.AddCommand(observeCmd) }
