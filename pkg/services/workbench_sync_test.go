package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func TestWorkbenchSyncSupersededAndIsolated(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	s.mu.Lock()
	s.workbenchReader = func(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		if c.Scope == "Old" {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return []common.WorkbenchTask{{SourceKey: "obsolete", Title: "Obsolete", State: "open"}}, nil
		}
		if c.Scope == "Fail" {
			return nil, errors.New("partial read")
		}
		return []common.WorkbenchTask{{SourceKey: "current", Title: "Current", State: "open"}}, nil
	}
	s.mu.Unlock()
	config := common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: t.TempDir(), Scope: "Old"}
	id, err := s.SaveWorkbench(config)
	if err != nil {
		t.Fatal(err)
	}
	await := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for sync")
		}
	}
	await(started)
	if err := s.SyncWorkbench(id); err != nil {
		t.Fatal(err)
	}
	first, err := s.WorkbenchSnapshot(id)
	if err != nil || len(first.Attempts) != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	other, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchGitHubIssues, Name: "Independent", Repository: "alswl/issues"})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(id, state string) common.WorkbenchSnapshot {
		t.Helper()
		for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
			c, err := s.WorkbenchSnapshot(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Attempts) > 0 && c.Attempts[0].State == state {
				return c
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("attempt for %s did not reach %s", id, state)
		return common.WorkbenchSnapshot{}
	}
	wait(other, "success")
	config = first.WorkbenchConnection
	config.Scope = "New"
	if _, err := s.SaveWorkbench(config); err != nil {
		t.Fatal(err)
	}
	await(cancelled)
	current := wait(id, "success")
	if current.Version != 2 || len(current.Attempts) < 2 || current.Attempts[1].State != "superseded" {
		t.Fatalf("%+v", current)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Current" {
		t.Fatalf("%+v %v", page, err)
	}
	s.mu.Lock()
	s.workbenchReader = func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		return []common.WorkbenchTask{{SourceKey: "partial", Title: "Partial"}}, errors.New("partial read")
	}
	s.mu.Unlock()
	if err := s.SyncWorkbench(id); err != nil {
		t.Fatal(err)
	}
	failed := wait(id, "failed")
	page, err = s.WorkbenchTasks(common.WorkbenchQuery{Connection: id, Revision: page.Revision})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Current" || !failed.LastSuccess.Equal(*current.LastSuccess) {
		t.Fatalf("%+v %+v %v", failed, page, err)
	}
}
