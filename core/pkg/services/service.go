package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type active struct {
	cancel          context.CancelFunc
	cancelRequested bool
}

type ObsidianSyncOptions struct {
	ScanInterval          time.Duration
	ScanTimeout           time.Duration
	MaxRetries            int
	ScanTimeoutConfigured bool
	Diagnostic            string
}

type Service struct {
	workbenchQueryPrograms workbenchQueryProgramCache

	volatileRevision      atomic.Uint64
	workbenchActive       map[string]context.CancelFunc
	workbenchDirty        map[string]bool
	workbenchPaths        map[string]map[string]bool
	workbenchWatchReady   map[string]workbenchWatchReadiness
	workbenchWatchWake    chan struct{}
	readProgress          sync.Map // attempt ID to common.WorkbenchReadProgress
	operationNotes        workbenchNoteGates
	operationRunning      map[string]bool
	githubRate            *integrations.WorkbenchGitHubRate
	githubCredentialMu    sync.Mutex
	githubCredentialGates sync.Map
	githubCredentials     integrations.WorkbenchCredentialStore
	githubBaseURL         string
	githubTransport       http.RoundTripper
	githubAppClientID     string
	githubAppSlug         string
	githubAuthBaseURL     string
	githubAuthorization   *githubAuthorizationManager
	githubNow             func() time.Time
	workbenchReader       workbenchReader
	obsidianSync          ObsidianSyncOptions
	workbenchPlugins      map[string]workbenchPlugin
	observers             map[string]common.Observer
	observing             map[string]active
	observerNext          map[string]time.Time
	closeOnce             sync.Once
	closeError            error
	suspended             bool
	mu                    sync.Mutex
	store                 Store
	dir                   string
	lock                  io.Closer
	ctx                   context.Context
	cancel                context.CancelFunc
	wg                    sync.WaitGroup
	tasks                 map[string]common.Task
	runs                  map[string]common.Run
	running               map[string]active
	settings              common.Settings
	stopping              bool
	lastError             string

	workbenchConnectorAuth map[string]*connectorAuthorization
	managedConnectors      sync.Map
	workbenchPluginMu      sync.Mutex
}

func New(dir string, openStore func(string) (Store, error)) (*Service, error) {
	return NewWithWorkbenchPlugins(dir, openStore, BuiltinWorkbenchPluginRegistry())
}

func NewWithWorkbenchPlugins(dir string, openStore func(string) (Store, error), registry WorkbenchPluginRegistry) (*Service, error) {
	return NewWithWorkbenchOptions(dir, openStore, registry, ObsidianSyncOptions{MaxRetries: 3})
}

func NewWithWorkbenchOptions(dir string, openStore func(string) (Store, error), registry WorkbenchPluginRegistry, options ObsidianSyncOptions) (*Service, error) {
	return NewWithWorkbenchDependencies(dir, openStore, registry, options, WorkbenchGitHubDependencies{})
}

// WorkbenchGitHubDependencies supplies optional adapters for the GitHub connector.
type WorkbenchGitHubDependencies struct {
	Credentials integrations.WorkbenchCredentialStore
	APIURL      string
	AuthURL     string
	Transport   http.RoundTripper
}

// NewWithWorkbenchDependencies opens a service with optional GitHub adapters.
func NewWithWorkbenchDependencies(dir string, openStore func(string) (Store, error), registry WorkbenchPluginRegistry, options ObsidianSyncOptions, github WorkbenchGitHubDependencies) (*Service, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return nil, e
	}
	lock, e := integrations.LockFile(filepath.Join(dir, "app.lock"))
	if errors.Is(e, integrations.ErrLocked) {
		return nil, common.Err("CONFLICT", "apilot is already running for this data directory")
	}
	if e != nil {
		return nil, e
	}

	db, e := openStore(filepath.Join(dir, "apilot.db"))
	if e != nil {
		lock.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{workbenchActive: make(map[string]context.CancelFunc), workbenchDirty: make(map[string]bool), workbenchPaths: make(map[string]map[string]bool), workbenchWatchReady: make(map[string]workbenchWatchReadiness), workbenchWatchWake: make(chan struct{}, 1), operationRunning: make(map[string]bool), workbenchPlugins: registry.plugins, observers: map[string]common.Observer{}, observing: map[string]active{}, observerNext: map[string]time.Time{}, store: db, dir: dir, lock: lock, ctx: ctx, cancel: cancel, tasks: map[string]common.Task{}, runs: map[string]common.Run{}, running: map[string]active{}}
	s.workbenchReader = s.readWorkbench
	s.githubCredentials, s.githubBaseURL, s.githubTransport = github.Credentials, github.APIURL, github.Transport
	s.githubAuthBaseURL = github.AuthURL
	s.githubAppClientID = GitHubAppClientID
	s.githubAppSlug = GitHubAppSlug
	s.githubRate = integrations.NewWorkbenchGitHubRate(4)
	s.obsidianSync = options
	s.lastError = options.Diagnostic
	fail := func(e error) (*Service, error) {
		cancel()
		s.closeWorkbenchConnector()
		db.Close()
		lock.Close()
		return nil, e
	}
	if err := s.registerBundledWorkbenchConnector(); err != nil {
		return fail(err)
	}
	if rates, ok := db.(interface {
		WorkbenchGitHubAccountBackoffs() (map[int64]time.Time, error)
		RecordWorkbenchGitHubAccountBackoff(int64, time.Time) error
	}); ok {
		backoffs, err := rates.WorkbenchGitHubAccountBackoffs()
		if err != nil {
			return fail(err)
		}
		for accountID, until := range backoffs {
			s.githubRate.RestoreBackoff(accountID, until)
		}
		s.githubRate.SetBackoffPersistence(func(accountID int64, until time.Time) {
			if err := rates.RecordWorkbenchGitHubAccountBackoff(accountID, until); err != nil {
				log.Printf("workbench: could not persist GitHub account backoff: %v", err)
			}
		})
	}
	if operations, ok := db.(interface{ RecoverWorkbenchTaskOperations() error }); ok {
		if err := operations.RecoverWorkbenchTaskOperations(); err != nil {
			return fail(err)
		}
	}
	s.settings, e = db.Settings()
	if e != nil {
		return fail(e)
	}
	ts, e := db.Tasks()
	if e != nil {
		return fail(e)
	}
	now := time.Now().UTC()
	for _, t := range ts {
		if e = s.resetSchedule(&t, now); e != nil {
			return fail(e)
		}
		s.tasks[t.ID] = t
	}
	rs, e := db.Runs()
	if e != nil {
		return fail(e)
	}
	for _, r := range rs {
		if r.Active() {
			r.Status = "interrupted"
			r.FinalizedAt = &now
			r.FinishedAt = nil
			r.DurationMS = nil
			r.ErrorCode = "APP_INTERRUPTED"
			r.Error = "App ended before the run was finalized"
			if e = db.SaveRun(r); e != nil {
				return fail(e)
			}
		}
		s.runs[r.ID] = r
	}
	observers, e := db.Observers()
	if e != nil {
		return fail(e)
	}
	for _, v := range observers {
		s.observers[v.ID] = v
		s.observerNext[v.ID] = now.Add(time.Duration(v.IntervalMS) * time.Millisecond)
	}
	if credentials, err := s.workbenchGitHubCredentials(); err == nil {
		s.wg.Add(1)
		go func() { defer s.wg.Done(); _ = s.cleanupWorkbenchGitHubCredentials(ctx, credentials) }()
	}
	s.wg.Add(1)
	go s.tick()
	if wb, ok := db.(workbenchStore); ok {
		// Queue startup scans before callers can create or cancel a scan.
		s.startScheduledWorkbench(wb, "startup")
		s.wg.Add(1)
		go s.tickWorkbench(wb)
		s.wg.Add(1)
		go s.watchWorkbenchConnections(wb)
	}
	if operations, ok := db.(workbenchTaskOperationStore); ok {
		s.wg.Add(1)
		go s.recoverWorkbenchOperations(operations)
	}
	return s, nil
}

// StopAdmission also cancels managed work before waiting for IPC or UI workers.
func (s *Service) StopAdmission() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	s.stopping = true
	s.cancel()
	for _, connector := range s.registeredConnectors() {
		connector.process.Stop()
	}
	if s.githubAuthorization != nil {
		s.githubAuthorization.close()
	}
	for id, a := range s.running {
		a.cancelRequested = true
		s.running[id] = a
		a.cancel()
	}
}

func (s *Service) Close() error {
	s.StopAdmission()
	s.closeOnce.Do(func() {
		s.closeWorkbenchConnector()
		s.wg.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closeError = s.store.Close()
		s.lock.Close()
	})
	return s.closeError
}

// VolatileRevision covers externally visible transitions outside SQLite.
func (s *Service) VolatileRevision() uint64 { return s.volatileRevision.Load() }

func (s *Service) Suspend() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.suspended {
		s.suspended = true
		s.volatileRevision.Add(1)
	}
}

func (s *Service) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []common.AttentionEvent
	if !s.stopping {
		events, _ = s.store.Events()
	}
	n := 0
	for _, e := range events {
		if e.AcknowledgedAt == nil {
			n++
		}
	}
	state := "ready"
	if s.stopping {
		state = "stopping"
	}
	return map[string]any{"app_version": common.Version, "protocol_version": 1, "state": state, "running_count": len(s.running), "observer_running_count": len(s.observing), "attention_count": n, "last_error": s.lastError}
}

func (s *Service) Doctor() map[string]any {
	checks := map[string]any{}
	for _, path := range []string{s.dir, filepath.Join(s.dir, "logs")} {
		st, e := os.Stat(path)
		checks[path] = map[string]any{"exists": e == nil, "directory": e == nil && st.IsDir()}
	}
	return map[string]any{"data_dir": s.dir, "schema_version": 4, "status": s.Status(), "path_checks": checks}
}

func (s *Service) String() string { return fmt.Sprintf("apilot(%s)", s.dir) }
