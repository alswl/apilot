package main

import "github.com/spf13/cobra"

var workbenchConnectorConnectionCmd = &cobra.Command{Use: "connection", Short: "Manage Connector accounts and scopes"}

func init() { workbenchConnectorCmd.AddCommand(workbenchConnectorConnectionCmd) }
