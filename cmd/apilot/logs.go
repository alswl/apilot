package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newLogsCommand(commandClient)) }

func newLogsCommand(c *client) *cobra.Command {
	logs := &cobra.Command{Use: "logs RUN_ID", Short: "Read stdout or stderr", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		stream, _ := cmd.Flags().GetString("stream")
		offset, _ := cmd.Flags().GetInt64("offset")
		follow, _ := cmd.Flags().GetBool("follow")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		for {
			b, e := c.call("logs.read", map[string]any{"run_id": a[0], "stream": stream, "offset": offset, "limit": 65536})
			if e != nil {
				return e
			}
			var chunk common.LogChunk
			if e = json.Unmarshal(b, &chunk); e != nil {
				return e
			}
			if c.json {
				if e = c.show(b); e != nil {
					return e
				}
			} else {
				if _, e = c.out.Write(chunk.Data); e != nil {
					return e
				}
			}
			offset = chunk.NextOffset
			if chunk.EOF || (!follow && len(chunk.Data) < 65536) {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(250 * time.Millisecond):
			}
		}
	}}
	logs.Flags().String("stream", "stdout", "stdout or stderr")
	logs.Flags().Int64("offset", 0, "Byte offset")
	logs.Flags().Bool("follow", false, "Follow until the run ends")
	return logs
}
