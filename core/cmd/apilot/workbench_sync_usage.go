package main

func init() {
	workbenchSyncCmd.AddCommand(workbenchSyncQueryCommand("usage", "Summarize sync usage by scope and mode"))
}
