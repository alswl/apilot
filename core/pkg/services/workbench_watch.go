package services

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type workbenchWatchHandle struct {
	version int64
	cancel  context.CancelFunc
}

type workbenchWatchReadiness struct {
	version   int64
	ready     chan struct{}
	signaled  bool
	requested bool
	installed bool
	verified  bool
	revision  uint64
	changed   map[string]bool
}

type workbenchWatchReadyKey struct{}

// requestWorkbenchWatchReadyLocked establishes the installation barrier for
// the connection version. An installation failure also releases the barrier;
// the normal full scan and periodic fallback then continue without a watch.
func (s *Service) requestWorkbenchWatchReadyLocked(connection common.WorkbenchConnection) <-chan struct{} {
	ready, ok := s.workbenchWatchReady[connection.ID]
	if !ok || ready.version != connection.Version {
		ready = workbenchWatchReadiness{version: connection.Version, ready: make(chan struct{}), requested: true}
		s.workbenchWatchReady[connection.ID] = ready
	}
	select {
	case s.workbenchWatchWake <- struct{}{}:
	default:
	}
	return ready.ready
}

func (s *Service) signalWorkbenchWatchReady(id string, version int64, installed bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ready, ok := s.workbenchWatchReady[id]
	if !ok {
		current, err := s.WorkbenchSnapshot(id)
		if err != nil || current.Version != version || current.Paused || current.PluginDisabled {
			return false
		}
		ready = workbenchWatchReadiness{version: version, ready: make(chan struct{})}
	}
	if ready.version != version {
		return false
	}
	ready.installed = installed
	if !installed {
		ready.verified = false
		ready.revision++
	}
	if ready.signaled {
		s.workbenchWatchReady[id] = ready
		return false
	}
	waitingScan := ready.requested
	close(ready.ready)
	ready.signaled = true
	s.workbenchWatchReady[id] = ready
	return waitingScan
}

// recordWorkbenchWatchEvent invalidates cross-note evidence before queuing the
// hint. A bounded path set permits revalidating the affected note independently.
func (s *Service) recordWorkbenchWatchEvent(id string, version int64, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ready, ok := s.workbenchWatchReady[id]
	if !ok || ready.version != version {
		return
	}
	ready.revision++
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		ready.verified = false
	} else if ready.verified {
		if ready.changed == nil {
			ready.changed = make(map[string]bool)
		}
		ready.changed[path] = true
		if len(ready.changed) > 64 {
			ready.verified = false
			ready.changed = nil
		}
	}
	s.workbenchWatchReady[id] = ready
}

func (s *Service) workbenchIdentityCoverageLocked(c common.WorkbenchConnection, path string) bool {
	ready, ok := s.workbenchWatchReady[c.ID]
	if !ok || ready.version != c.Version || !ready.installed || !ready.verified {
		return false
	}
	for changed := range ready.changed {
		if changed != path {
			return false
		}
	}
	return true
}

// watchWorkbenchConnections maintains subscriptions for active configurations.
// Every installation emits a full-read hint, including after lost coverage.
func (s *Service) watchWorkbenchConnections(st workbenchStore) {
	defer s.wg.Done()
	handles := map[string]workbenchWatchHandle{}
	var workers sync.WaitGroup
	defer func() {
		for _, h := range handles {
			h.cancel()
		}
		workers.Wait()
	}()
	reconcile := func() {
		connections, err := st.WorkbenchConnections()
		if err != nil {
			return
		}
		wanted := map[string]common.WorkbenchConnection{}
		for _, snapshot := range connections {
			if snapshot.Kind != common.WorkbenchObsidianTasks || snapshot.Paused || !filepath.IsAbs(snapshot.Vault) {
				continue
			}
			enabled, err := s.workbenchPluginEnabled(snapshot.Kind)
			if err == nil && enabled {
				wanted[snapshot.ID] = snapshot.WorkbenchConnection
			}
		}
		for id, handle := range handles {
			connection, ok := wanted[id]
			if !ok || connection.Version != handle.version {
				handle.cancel()
				delete(handles, id)
				s.mu.Lock()
				if ready, exists := s.workbenchWatchReady[id]; exists && ready.version == handle.version {
					delete(s.workbenchWatchReady, id)
				}
				s.mu.Unlock()
			}
		}
		for id, connection := range wanted {
			if _, ok := handles[id]; ok {
				continue
			}
			ctx, cancel := context.WithCancel(s.ctx)
			handles[id] = workbenchWatchHandle{version: connection.Version, cancel: cancel}
			workers.Add(1)
			go func() {
				defer workers.Done()
				lostReported := false
				for ctx.Err() == nil {
					hints := make(chan string, 64)
					overflow := make(chan struct{}, 1)
					done := make(chan bool, 1)
					go func() {
						defer func() {
							waitingScan := false
							if ctx.Err() == nil {
								waitingScan = s.signalWorkbenchWatchReady(connection.ID, connection.Version, false)
							}
							done <- waitingScan
						}()
						_ = integrations.WatchWorkbenchObsidian(ctx, connection, func(path string) {
							if path == "" && s.signalWorkbenchWatchReady(connection.ID, connection.Version, true) {
								// The waiting scan starts only after installation and covers
								// changes that occurred before the watch was ready.
								return
							}
							if path != "" {
								s.recordWorkbenchWatchEvent(connection.ID, connection.Version, path)
							}
							select {
							case hints <- path:
							default:
								s.recordWorkbenchWatchEvent(connection.ID, connection.Version, "")
								select {
								case overflow <- struct{}{}:
								default:
								}
							}
						})
					}()
					watching := true
					waitingScan := false
					for watching {
						select {
						case <-ctx.Done():
							<-done
							return
						case waitingScan = <-done:
							watching = false
						case <-overflow:
							s.enqueueWorkbenchSourceChange(st, connection.ID)
						case path := <-hints:
							lostReported = false
							s.enqueueWorkbenchPathChange(st, connection.ID, path)
						}
					}
					if ctx.Err() != nil {
						return
					}
					if !lostReported {
						if !waitingScan {
							s.enqueueWorkbenchSourceChange(st, connection.ID)
						}
						lostReported = true
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Second):
					}
				}
			}()
		}
	}
	reconcile()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		case <-s.workbenchWatchWake:
			reconcile()
		}
	}
}
