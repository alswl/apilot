package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/alswl/apilot/core/pkg/common"
)

func init() {
	var connection string
	cmd := &cobra.Command{Use: "login CONNECTOR_ID", Short: "Start Connector browser login", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := common.WorkbenchConnectorLoginInput{ConnectorID: args[0], ConnectionID: connection}
			if commandClient.json {
				return showWorkbench("workbench.connector.login", in)
			}
			raw, err := commandClient.call("workbench.connector.login", in)
			if err != nil {
				return err
			}
			var status common.WorkbenchConnectorAuthorization
			if err := json.Unmarshal(raw, &status); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			tick := time.NewTicker(350 * time.Millisecond)
			defer tick.Stop()
			shown := ""
			query := map[string]string{"id": status.ID}
			for status.Status == "waiting" {
				if status.VerificationURL != "" && status.VerificationURL != shown {
					shown = status.VerificationURL
					fmt.Fprintln(cmd.ErrOrStderr(), "Open", shown)
					if status.UserCode != "" {
						fmt.Fprintln(cmd.ErrOrStderr(), "Code", status.UserCode)
					}
				}
				select {
				case <-ctx.Done():
					_, _ = commandClient.call("workbench.connector.cancel-login", query)
					return common.Err("CONFLICT", "Connector login cancelled")
				case <-tick.C:
				}
				raw, err = commandClient.call("workbench.connector.auth-status", query)
				if err != nil {
					return err
				}
				status = common.WorkbenchConnectorAuthorization{}
				if err := json.Unmarshal(raw, &status); err != nil {
					return err
				}
			}
			if status.Status != "authorized" {
				return common.Err("CONFLICT", fmt.Sprintf("Connector login %s (%s)", status.Status, status.ErrorKind))
			}
			return commandClient.show(status)
		}}
	cmd.Flags().StringVar(&connection, "connection", "", "Reauthorize the account for this connection")
	workbenchConnectorCmd.AddCommand(cmd)
}
