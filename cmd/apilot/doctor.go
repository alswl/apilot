package main

import (
	"encoding/json"
	"os"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/config"
	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newDoctorCommand(commandClient)) }

func newDoctorCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "doctor", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		data, e := c.call("doctor", nil)
		if e == nil {
			return c.show(data)
		}
		paths := map[string]any{}
		for _, p := range []string{config.DataDir(), config.DataDir() + "/logs"} {
			st, err := os.Stat(p)
			paths[p] = map[string]any{"exists": err == nil, "directory": err == nil && st.IsDir()}
		}
		if c.json {
			_ = json.NewEncoder(c.out).Encode(common.Response{Version: 1, Data: map[string]any{"paths": paths}, Error: common.ProtocolError(e)})
			return &reported{e}
		}
		_ = c.show(paths)
		return e
	}}
}
