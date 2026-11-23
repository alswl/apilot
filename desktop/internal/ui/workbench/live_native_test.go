//go:build darwin

package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/server"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo"
)

func TestMain(tests *testing.M) {
	cli := os.Getenv("APILOT_TEST_LIVE_CLI")
	if cli == "" {
		os.Exit(tests.Run())
	}
	if err := probeLiveCLI(cli); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func probeLiveCLI(cli string) error {
	dir, err := os.MkdirTemp(os.Getenv("APILOT_TEST_ARTIFACT_DIR"), "sse-native-")
	if err != nil {
		return err
	}
	if err := os.Setenv("APILOT_DATA_DIR", dir); err != nil {
		return err
	}
	m := newFramework()
	m.live = services.NewRemote(config.SocketPath(dir))
	m.page = tasksPage
	finished := make(chan error, 1)
	mygo.App.OnReady(func() {
		go func() {
			defer mygo.RunOnMain(func() { mygo.App.Quit() })
			finished <- exerciseLiveCLI(cli, dir, m)
		}()
	})
	if err := runWorkspace(m, "", "", "en"); err != nil {
		return err
	}
	select {
	case err := <-finished:
		return err
	default:
		return errors.New("native Workbench exited before the CLI probe completed")
	}
}

func exerciseLiveCLI(cli, dir string, m *mockup) error {
	wait := func(label string, predicate func() bool) error {
		for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
			result := make(chan bool, 1)
			mygo.RunOnMain(func() { result <- predicate() })
			select {
			case ready := <-result:
				if ready {
					fmt.Println(label)
					return nil
				}
			case <-time.After(time.Second):
				return errors.New("native Workbench event loop stopped")
			}
		}
		return fmt.Errorf("native Workbench did not reach %s", label)
	}
	if err := wait("native Workbench reported unavailable with no initial socket", func() bool {
		return m.liveDisconnected && m.liveError && !m.liveLoaded
	}); err != nil {
		return err
	}
	core, err := server.Open(dir)
	if err != nil {
		return err
	}
	defer core.Close()
	if err := wait("native Workbench loaded the initial snapshot", func() bool {
		return m.liveLoaded && len(m.objects) == 0
	}); err != nil {
		return err
	}
	create := func(requestID, title string) error {
		input, _ := json.Marshal(common.WorkbenchTaskWrite{
			RequestID: requestID, ConnectionID: common.DefaultWorkbenchLocalID, ConnectionVersion: 1, Title: title,
		})
		cmd := exec.Command(cli, "--json", "workbench", "task", "operation", "submit", "-")
		cmd.Stdin = strings.NewReader(string(input))
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("CLI submission failed: %w: %s", err, output)
		}
		return nil
	}
	if err := create("native-first", "CLI event task"); err != nil {
		return err
	}
	if err := wait("native Workbench displayed the CLI task without manual refresh", func() bool {
		return len(m.objects) == 1 && m.objects[0].Title == "CLI event task"
	}); err != nil {
		return err
	}
	if err := core.Close(); err != nil {
		return err
	}
	if err := wait("native Workbench retained the task and marked it stale during disconnect", func() bool {
		return m.liveDisconnected && m.liveStale && len(m.objects) == 1
	}); err != nil {
		return err
	}
	restarted, err := server.Open(dir)
	if err != nil {
		return err
	}
	defer restarted.Close()
	if err := create("native-second", "CLI restart task"); err != nil {
		return err
	}
	return wait("native Workbench reconnected and read both tasks", func() bool {
		return !m.liveStale && len(m.objects) == 2
	})
}
