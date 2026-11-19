package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectionCmd.AddCommand(&cobra.Command{
		Use:  "headings CONNECTION_ID NOTE",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connection.headings", map[string]string{"id": args[0], "note_path": args[1]})
		},
	})
}
