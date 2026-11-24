package main

import "github.com/spf13/cobra"

var workbenchSyncCmd = &cobra.Command{Use: "sync", Short: "Run synchronization and query results"}

func init() { workbenchCmd.AddCommand(workbenchSyncCmd) }
