package main

import "github.com/spf13/cobra"

var workbenchConnectionCmd = &cobra.Command{Use: "connection", Short: "Configure and query workbench connections"}

func init() {
	group := workbenchConnectionCmd
	group.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		return showWorkbench("workbench.connection.list", nil)
	}})
	group.AddCommand(
		&cobra.Command{Use: "save FILE|-", Short: "Create or update a connection from JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.connection.save", args[0])
		}},
		&cobra.Command{Use: "show CONNECTION_ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connection.show", map[string]string{"id": args[0]})
		}},
		&cobra.Command{Use: "remove CONNECTION_ID", Short: "Remove a connection and its local tasks", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connection.remove", map[string]string{"id": args[0]})
		}},
	)
	workbenchCmd.AddCommand(group)
}
