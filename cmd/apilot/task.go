package main

import (
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{Use: "task", Short: "Create and manage tasks"}

func init() { rootCmd.AddCommand(taskCmd) }
