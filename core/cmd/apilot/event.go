package main

import (
	"github.com/spf13/cobra"
)

var eventCmd = &cobra.Command{Use: "event", Short: "Review attention events"}

func init() { rootCmd.AddCommand(eventCmd) }
