package main

import "github.com/spf13/cobra"

func init() { taskCmd.AddCommand(newTaskAddCommand(commandClient)) }

func newTaskAddCommand(c *client) *cobra.Command {
	cmd := &cobra.Command{Use: "add TASK [flags] -- EXECUTABLE [ARGS...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error { return c.saveTaskInput("add", cmd, a) }}
	configureTaskInputFlags(cmd)
	return cmd
}
