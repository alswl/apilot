package main

import "github.com/spf13/cobra"

func init() { taskCmd.AddCommand(newTaskUpdateCommand(commandClient)) }

func newTaskUpdateCommand(c *client) *cobra.Command {
	cmd := &cobra.Command{Use: "update TASK [flags] -- EXECUTABLE [ARGS...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error { return c.saveTaskInput("update", cmd, a) }}
	configureTaskInputFlags(cmd)
	return cmd
}
