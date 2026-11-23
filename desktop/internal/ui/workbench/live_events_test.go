package workbench

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/config"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func TestLiveEventsQueueRefreshWhileBusy(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.liveBusy = true
	m.receiveLiveEvent(common.WorkbenchEvent{Instance: "a", Sequence: 2, Type: "changed"})
	if !m.liveQueued {
		t.Fatal("change was lost during a refresh")
	}
	m.receiveLiveDisconnect()
	if !m.liveStale {
		t.Fatal("disconnected data was presented as current")
	}
	m.receiveLiveEvent(common.WorkbenchEvent{Instance: "b", Sequence: 1, Type: "snapshot"})
	if !m.liveQueued || m.liveDisconnected || !m.liveStale || m.liveGeneration != 1 {
		t.Fatal("reconnect did not fence the old read and queue a full reread")
	}
}

func TestLiveVisibilityClearsBeforeTaskReadSucceeds(t *testing.T) {
	m := newDevEventScenario()
	m.devEvents = nil
	m.liveStale = true
	m.liveError = true
	m.newTaskSourceID, m.newTaskDraft, m.newTaskOpen = "dev-github", "protected", true
	m.taskDetailUI("dev-issue").draft = "protected annotation"
	m.taskDetailUI("dev-task").draft = "local annotation"
	m.serverTaskOperations["dev-issue"] = common.WorkbenchTaskReceipt{ConnectionID: "dev-github", RequestID: "protected"}
	m.syncDetail = m.liveConnections[1]
	m.syncLogs["dev-github"] = []syncEvent{{message: "private log"}}
	m.syncLogs["dev-local"] = []syncEvent{{message: "local log"}}
	snapshot := m.liveConnections[1]
	snapshot.AccessState = "pending"
	snapshot.Version++
	if !m.clearChangedGitHubAccess([]common.WorkbenchSnapshot{m.liveConnections[0], snapshot}) {
		t.Fatal("confirmed authorization transition not detected")
	}
	if len(m.objects) != 1 || m.objects[0].ID != "dev-task" || m.taskDetails["dev-task"].draft != "local annotation" {
		t.Fatal("unaffected connection contents were cleared")
	}
	if len(m.syncLogs["dev-github"]) != 0 || len(m.syncLogs["dev-local"]) != 1 {
		t.Fatal("sync logs did not respect the connection boundary")
	}
	if _, exists := m.taskDetails["dev-issue"]; exists || len(m.serverTaskOperations) != 0 || m.syncDetail.ID != "" || m.newTaskOpen {
		t.Fatal("old account content survived a confirmed transition")
	}
}

func TestLiveAnnotationInvalidationPreservesDraft(t *testing.T) {
	m := newFramework()
	detail := m.taskDetailUI("task")
	detail.annotationsLoading = true
	detail.draft = "unsent"
	if !m.invalidateLiveAnnotations() || detail.annotationsLoading || detail.annotationGeneration != 1 || detail.draft != "unsent" {
		t.Fatal("annotation cache was not invalidated independently of the draft")
	}
}

func TestSecondConnectionReadFencesBlockedRefresh(t *testing.T) { testBlockedAccessRead(t, false) }
func TestSyncDetailRevocationFencesBlockedRefresh(t *testing.T) { testBlockedAccessRead(t, true) }

func testBlockedAccessRead(t *testing.T, detailFirst bool) {
	t.Helper()
	m := newDevEventScenario()
	m.devEvents = nil
	m.newTaskSourceID, m.newTaskDraft, m.newTaskOpen = "dev-github", "protected", true
	m.syncLogID = "dev-github"
	before := append([]common.WorkbenchSnapshot(nil), m.liveConnections...)
	after := append([]common.WorkbenchSnapshot(nil), before...)
	after[1].AccessState = "pending"
	if detailFirst {
		after[1].AccessState = "unavailable"
		m.syncLogs["dev-github"] = []syncEvent{{message: "old error"}}
		m.syncLogs["dev-local"] = []syncEvent{{message: "local"}}
		m.pendingSync["dev-github"], m.pendingSync["dev-local"] = true, true
		m.connectionByID("dev-github").ActiveAttempt = 7
		m.connectionByID("dev-github").ReadProgress = &common.WorkbenchReadProgress{}
		m.connectionByID("dev-github").LastConfirmedAt = "yesterday"
		m.serverTaskOperations["dev-issue"] = common.WorkbenchTaskReceipt{ConnectionID: "dev-github", RequestID: "protected"}
	} else {
		after[1].Version++
	}
	tasks := append([]sourceObject(nil), m.objects...)

	socket := config.HTTPSocketPath(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var lists, details atomic.Int32
	blocked, release := make(chan struct{}), make(chan struct{})
	fixture := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/workbench/connections":
			items := before
			if lists.Add(1) == 2 && !detailFirst {
				items = after
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case "/v1/workbench/connections/dev-github/sync":
			if detailFirst {
				_ = json.NewEncoder(w).Encode(after[1])
				return
			}
			if details.Add(1) == 2 {
				close(blocked)
				<-release
			}
			_ = json.NewEncoder(w).Encode(before[1])
		case "/v1/workbench/tasks":
			_ = json.NewEncoder(w).Encode(common.WorkbenchPage{Items: tasks, Total: len(tasks)})
		case "/v1/workbench/plugins":
			if detailFirst {
				close(blocked)
				<-release
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = fixture.Serve(listener) }()
	defer fixture.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	m.live = services.NewRemote(filepath.Join(filepath.Dir(socket), "ipc.sock"))
	actions := make(chan func(), 4)
	m.refreshLiveWithDispatch(func(fn func()) { actions <- fn })
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh never reached its blocked follow-up request")
	}
	m.liveGeneration++
	for range 2 {
		select {
		case apply := <-actions:
			apply()
		default:
			t.Fatal("authorization confirmation waited for the follow-up request")
		}
	}
	if m.newTaskOpen || m.newTaskDraft != "" || len(m.objects) != 1 || m.objects[0].ID != "dev-task" {
		t.Fatal("authorization read failed to purge old account state")
	}
	if detailFirst {
		conn := m.connectionByID("dev-github")
		if conn.ActiveAttempt != 0 || conn.ReadProgress != nil || conn.LastConfirmedAt == "yesterday" ||
			m.pendingSync[conn.ID] || len(m.syncLogs[conn.ID]) != 0 || len(m.serverTaskOperations) != 0 {
			t.Fatal("revoked connection retained progress, logs, or receipts")
		}
		if !m.pendingSync["dev-local"] || len(m.syncLogs["dev-local"]) != 1 {
			t.Fatal("revocation affected the sibling connection")
		}
		close(release)
		select {
		case apply := <-actions:
			apply()
		case <-time.After(2 * time.Second):
			t.Fatal("failed follow-up did not complete")
		}
		if len(m.objects) != 1 || len(m.serverTaskOperations) != 0 || m.newTaskOpen {
			t.Fatal("late result restored access")
		}
	}
}

func TestSyncDetailMustMatchLatestConnectionAuthorization(t *testing.T) {
	m := newDevEventScenario()
	old := m.liveConnections[1]
	current := old
	current.Version++
	current.AccessState = "pending"
	if matchesLiveGitHubAccess([]common.WorkbenchSnapshot{current}, old) ||
		matchesLiveGitHubAccess(nil, old) ||
		!matchesLiveGitHubAccess([]common.WorkbenchSnapshot{current}, current) {
		t.Fatal("late sync details can cross authorization generations")
	}
}

func TestInitialStreamFailureIsUnavailableUntilSnapshot(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.liveBusy = true
	m.receiveLiveDisconnect()
	if !m.liveError || m.liveLoaded {
		t.Fatal("first stream failure still looks like loading")
	}
	m.receiveLiveEvent(common.WorkbenchEvent{Type: "snapshot", Instance: "recovered", Sequence: 1})
	if !m.liveQueued || m.liveDisconnected {
		t.Fatal("recovered stream did not request an initial read")
	}
}

func TestLiveAnnotationReadRecoversWithoutAnotherEvent(t *testing.T) {
	attempts := 0
	items, err := readLiveAnnotations(func() ([]common.WorkbenchAnnotation, error) {
		attempts++
		if attempts == 1 {
			return nil, common.Err("IPC_UNAVAILABLE", "temporary")
		}
		return []common.WorkbenchAnnotation{{Body: "new annotation"}}, nil
	})
	if err != nil || attempts != 2 || len(items) != 1 || items[0].Body != "new annotation" {
		t.Fatalf("bounded retry did not retrieve fresh annotations: %+v, %v (%d)", items, err, attempts)
	}
	attempts = 0
	_, _ = readLiveAnnotations(func() ([]common.WorkbenchAnnotation, error) {
		attempts++
		return nil, common.Err("FORBIDDEN", "revoked")
	})
	if attempts != 1 {
		t.Fatal("access denial was retried")
	}
}

func TestPermissionRetryBudgetSurvivesChangedEventsAndResetsOnReconnect(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.liveBusy = true
	for range 3 {
		delay, ok := m.nextPermissionRetry()
		if !ok || delay < 500*time.Millisecond {
			t.Fatalf("unexpected retry schedule: %v %t", delay, ok)
		}
		m.receiveLiveEvent(common.WorkbenchEvent{Type: "changed"})
	}
	if _, ok := m.nextPermissionRetry(); ok || !m.liveQueued {
		t.Fatal("ordinary event failed to queue recovery or incorrectly reset the conflict budget")
	}
	m.receiveLiveDisconnect()
	if _, ok := m.nextPermissionRetry(); ok {
		t.Fatal("disconnected state scheduled a retry")
	}
	m.receiveLiveEvent(common.WorkbenchEvent{Type: "snapshot", Instance: "reconnected", Sequence: 1})
	if delay, ok := m.nextPermissionRetry(); !ok || delay == 0 {
		t.Fatal("reconnect failed to restore the retry budget")
	}
}
