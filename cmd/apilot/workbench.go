package main

import (
	"encoding/json"
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

var workbenchCmd = &cobra.Command{Use: "workbench", Short: "Read synced workbench data"}

func init() { rootCmd.AddCommand(workbenchCmd) }

func showWorkbench(method string, params any) error {
	data, err := commandClient.call(method, params)
	if err != nil {
		return err
	}
	if commandClient.json {
		return commandClient.show(data)
	}
	var formatted any
	if err := json.Unmarshal(data, &formatted); err != nil {
		return err
	}
	switch method {
	case "workbench.task.list":
		var page common.WorkbenchPage
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		for _, task := range page.Items {
			if _, err := fmt.Fprintf(commandClient.out, "%s\t%s\t%s\t%s\n", task.ID, task.State, task.Mode, task.Title); err != nil {
				return err
			}
		}
		return nil
	case "workbench.connection.list":
		var list struct{ Items []common.WorkbenchSnapshot }
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		for _, c := range list.Items {
			if _, err := fmt.Fprintf(commandClient.out, "%s\t%s\t%s\n", c.ID, c.Mode, c.Name); err != nil {
				return err
			}
		}
		return nil
	default:
		b, err := json.MarshalIndent(formatted, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(commandClient.out, string(b))
		return err
	}
}
