package main

import "github.com/spf13/cobra"

var workbenchGitHubAuthCmd = &cobra.Command{Use: "authorization", Short: "Manage GitHub device authorization"}

func init() { workbenchGitHubCmd.AddCommand(workbenchGitHubAuthCmd) }
