package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandClient = &client{}
var rootCmd = &cobra.Command{Use: "apilot", Short: "Manage local tasks in the running apilot App", SilenceErrors: true, SilenceUsage: true}
var commandMu sync.Mutex

func init() {
	rootCmd.PersistentFlags().BoolVar(&commandClient.json, "json", false, "Output versioned JSON")
}
func Execute() int { return execute(os.Args[1:], os.Stdout, os.Stderr) }
func resetCommand(cmd *cobra.Command) {
	cmd.SetContext(context.Background())
	reset := func(f *pflag.Flag) {
		if slice, ok := f.Value.(pflag.SliceValue); ok {
			_ = slice.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	cmd.Flags().Init(cmd.Name(), pflag.ContinueOnError)
	cmd.Flags().VisitAll(reset)
	cmd.PersistentFlags().VisitAll(reset)
	for _, child := range cmd.Commands() {
		resetCommand(child)
	}
}
func execute(args []string, out, errout io.Writer) int {
	commandMu.Lock()
	defer commandMu.Unlock()
	resetCommand(rootCmd)
	c := commandClient
	c.path = config.SocketPath(config.DataDir())
	c.out = out
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" {
			c.json = true
		}
	}
	rootCmd.SetOut(out)
	rootCmd.SetErr(errout)
	rootCmd.SetArgs(args)
	if e := rootCmd.Execute(); e != nil {
		if r, ok := e.(*reported); ok {
			return exitCode(common.ProtocolError(r.err))
		}
		v := common.ProtocolError(e)
		var domainError *common.Error
		if !errors.As(e, &domainError) {
			v.Code = "INVALID_ARGUMENT"
		}
		if c.json {
			_ = json.NewEncoder(c.out).Encode(common.Response{Version: 1, Error: v})
		} else {
			fmt.Fprintf(errout, "%s: %s\n", v.Code, v.Message)
		}
		return exitCode(v)
	}
	return 0
}
