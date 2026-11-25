package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/dal"
)

type connectorMappingBarrier struct {
	*dal.Store
	armed           atomic.Bool
	entered, resume chan struct{}
}

func (s *connectorMappingBarrier) WorkbenchConnectorMembers(id string) ([]common.WorkbenchTask, error) {
	tasks, err := s.Store.WorkbenchConnectorMembers(id)
	if s.armed.Swap(false) {
		close(s.entered)
		<-s.resume
	}
	return tasks, err
}

func TestConnectorMappingRejectsChangedProcess(t *testing.T) {
	t.Setenv("APILOT_DEV_CONNECTOR_MANIFEST", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	packages, dir := t.TempDir(), t.TempDir()
	const id = "synthetic-scopes"
	if out, err := exec.CommandContext(ctx, "bash", "../../../scripts/build-scoped-connector-fixture.sh", packages).CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v %s", err, out)
	}
	trust, err := os.ReadFile(filepath.Join(packages, "connector-publishers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "connector-publishers.json"), trust, 0600); err != nil {
		t.Fatal(err)
	}
	barrier := &connectorMappingBarrier{entered: make(chan struct{}), resume: make(chan struct{})}
	s, err := New(dir, func(path string) (Store, error) {
		store, err := dal.Open(path)
		barrier.Store = store
		return barrier, err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.ImportWorkbenchConnector(ctx, filepath.Join(packages, id+"-0.1.0.apilotconnector")); err != nil {
		t.Fatal(err)
	}
	auth, err := s.StartWorkbenchConnectorAuthorization(common.WorkbenchConnectorLoginInput{ConnectorID: id})
	if err != nil {
		t.Fatal(err)
	}
	for auth.Status == "waiting" {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(15 * time.Millisecond):
		}
		auth, err = s.WorkbenchConnectorAuthorization(auth.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if auth.Status != "authorized" {
		t.Fatalf("authorization: %+v", auth)
	}
	result, err := s.SaveWorkbenchConnectorConnection(ctx, common.WorkbenchConnectorSaveInput{
		AuthorizationID: auth.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1,
		Scopes: []common.WorkbenchConnectorSelection{{ScopeType: protocol.ScopeProject, Config: map[string]any{"target": "alpha"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	idScope := result.Scopes[0].ScopeID
	var baseline common.WorkbenchConnectorStoredScope
	for {
		baseline, err = barrier.WorkbenchConnectorScope(idScope)
		if err != nil {
			t.Fatal(err)
		}
		if baseline.AccessState == "available" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(15 * time.Millisecond):
		}
	}
	barrier.armed.Store(true)
	resultErr := make(chan error, 1)
	go func() {
		_, err := s.WorkbenchConnectorMapping(ctx, idScope, &common.WorkbenchMappingInput{Revision: baseline.MappingRevision, RestoreDefaults: true}, true)
		resultErr <- err
	}()
	released := false
	defer func() {
		if !released {
			close(barrier.resume)
		}
	}()
	select {
	case <-barrier.entered:
	case err := <-resultErr:
		t.Fatalf("mapping returned before lifecycle change: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for _, enabled := range []bool{false, true} {
		if _, err := s.SetWorkbenchPluginEnabled(id, enabled); err != nil {
			t.Fatal(err)
		}
	}
	close(barrier.resume)
	released = true
	select {
	case err := <-resultErr:
		if err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
			t.Fatalf("stale process mapping save: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	after, err := barrier.WorkbenchConnectorScope(idScope)
	if err != nil || after.MappingRevision != baseline.MappingRevision {
		t.Fatalf("mapping changed: %+v %v", after, err)
	}
}
