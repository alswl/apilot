package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/spf13/cobra"
)

var watchComplete = errors.New("snapshot received")

func init() {
	var once bool
	cmd := &cobra.Command{
		Use: "watch", Short: "Stream workbench invalidations as JSON lines", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			remote := services.NewRemote(commandClient.path)
			encoder := json.NewEncoder(commandClient.out)
			for {
				var outputErr error
				err := remote.WatchWorkbenchEvents(ctx, func(event common.WorkbenchEvent) error {
					outputErr = writeWatchEvent(ctx, encoder, event)
					if outputErr == nil && once {
						return watchComplete
					}
					return outputErr
				})
				if errors.Is(err, watchComplete) || ctx.Err() != nil {
					return nil
				}
				if outputErr != nil {
					return reportWatchError(ctx, cmd, common.Err("IPC_UNAVAILABLE", "Could not write event output: "+outputErr.Error()))
				}
				if errors.Is(err, io.EOF) {
					err = common.Err("IPC_UNAVAILABLE", "Apilot closed the event stream")
				}
				switch problem := common.ProtocolError(err); problem.Code {
				case "APP_NOT_RUNNING", "IPC_UNAVAILABLE", "IPC_TIMEOUT":
					if once {
						return reportWatchError(ctx, cmd, problem)
					}
				default:
					return reportWatchError(ctx, cmd, problem)
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "Read one initial snapshot without retrying and exit")
	workbenchCmd.AddCommand(cmd)
}

func writeWatchEvent(ctx context.Context, encoder *json.Encoder, event common.WorkbenchEvent) error {
	return writeWatchOutput(ctx, func() error {
		return encoder.Encode(common.Response{Version: 1, Data: event})
	})
}

func writeWatchOutput(ctx context.Context, write func() error) error {
	// An inherited pipe may be blocked in the kernel. The CLI can abandon its
	// single pending write on signal and exit without queueing output.
	done := make(chan error, 1)
	go func() { done <- write() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func reportWatchError(ctx context.Context, cmd *cobra.Command, problem *common.Error) error {
	stderr, jsonOutput := cmd.ErrOrStderr(), commandClient.json
	_ = writeWatchOutput(ctx, func() error {
		if jsonOutput {
			return json.NewEncoder(stderr).Encode(common.Response{Version: 1, Error: problem})
		}
		_, err := fmt.Fprintf(stderr, "%s: %s\n", problem.Code, problem.Message)
		return err
	})
	if ctx.Err() != nil {
		return nil
	}
	return &reported{err: problem}
}
