package main

import "github.com/spf13/cobra"

var workbenchGitHubCmd = &cobra.Command{Use: "github", Short: "Manage GitHub authorization and repository discovery"}

func init() { workbenchCmd.AddCommand(workbenchGitHubCmd) }
