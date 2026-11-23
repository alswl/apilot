package services

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"sort"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

const connectorDeltaInterval = 5 * time.Minute
const connectorFullInterval = 24 * time.Hour
const connectorMaxPages = 10000

type connectorRunCounter struct {
	last *protocol.Usage
}

func (c *connectorRunCounter) notify(msg protocol.Message) error {
	if msg.Method != "sync.usage" {
		return nil
	}
	var usage protocol.Usage
	if json.Unmarshal(msg.Params, &usage) != nil || !validConnectorUsage(usage) {
		return integrations.ErrWorkbenchConnectorProtocol
	}
	c.last = &usage
	return nil
}

func validConnectorUsage(u protocol.Usage) bool {
	return u.Requests >= 0 && u.Pages >= 0 && u.Received >= 0 && u.RateLimits >= 0 &&
		u.Requests <= 1_000_000 && u.Pages <= 1_000_000 && u.Received <= 1_000_000 && u.RateLimits <= 1_000_000
}

func connectorUsage(run *common.WorkbenchSyncRun, u protocol.Usage) {
	run.Requests += u.Requests
	run.Pages += u.Pages
	run.Fetched += u.Received
	run.RateLimits += u.RateLimits
}

func connectorFailedUsage(run *common.WorkbenchSyncRun, counter connectorRunCounter, err error) {
	var rpc *protocol.RPCError
	if errors.As(err, &rpc) && rpc.Data.Usage != nil && validConnectorUsage(*rpc.Data.Usage) {
		connectorUsage(run, *rpc.Data.Usage)
		return
	}
	if counter.last != nil {
		connectorUsage(run, *counter.last)
	}
	run.UsageComplete = false
}

func (s *Service) StartWorkbenchConnectorSync(id string, full bool) ([]common.WorkbenchConnectorScopeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.managedConnectorStore()
	if err != nil {
		return nil, err
	}
	overview, err := store.WorkbenchConnectorOverviews(id)
	if err != nil {
		return nil, err
	}
	if len(overview) != 1 {
		return nil, common.Err("NOT_FOUND", "Unknown Connector connection")
	}
	items := make([]common.WorkbenchConnectorScopeResult, 0, len(overview[0].Scopes))
	for _, scope := range overview[0].Scopes {
		item := common.WorkbenchConnectorScopeResult{ScopeID: scope.ScopeID, ScopeRef: scope.ScopeRef, Label: scope.Label, Status: "queued"}
		if _, err := s.startWorkbenchConnectorRunLocked(store, scope.ScopeID, "manual", full); err != nil {
			item.Status, item.ErrorKind = "sync_not_started", connectorErrorKind(err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) startWorkbenchConnectorRunLocked(store workbenchManagedConnectorStore, id, trigger string, full bool) (int64, error) {
	if s.stopping {
		return 0, common.Err("CONFLICT", "App is stopping")
	}
	scope, err := store.WorkbenchConnectorScope(id)
	if err != nil {
		return 0, err
	}
	process, err := s.managedConnector(scope.ConnectorID)
	if err != nil {
		return 0, err
	}
	if scope.Config.Paused || scope.AuthState != "authorized" {
		return 0, common.Err("CONFLICT", "Connector scope is paused or authorization is required")
	}
	if s.workbenchActive[id] != nil {
		return 0, common.Err("CONFLICT", "Connector scope sync is already running")
	}
	now := time.Now().UTC()
	if until, err := store.WorkbenchConnectorBackoff(scope.ConnectionID); err != nil {
		return 0, err
	} else if until != nil && now.Before(*until) {
		return 0, protocol.Problem(protocol.RateLimited, "account", "Connector backoff is active")
	}
	mode := "delta"
	if full || scope.Checkpoint == "" || scope.AccessState != "available" || scope.LastFullSuccess == nil || now.Sub(*scope.LastFullSuccess) >= connectorFullInterval {
		mode = "full"
	}
	run := common.WorkbenchSyncRun{ID: common.ID(), ScopeID: id, Trigger: trigger, Mode: mode, Status: "running", StartedAt: now, UsageComplete: true}
	legacyID, err := store.StartWorkbenchConnectorRun(run)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	s.workbenchActive[id] = cancel
	commit := common.WorkbenchConnectorSyncCommit{Scope: scope, Run: run, PluginID: s.managedPluginID(scope.ConnectorID), Release: *s.workbenchConnectorRelease,
		Records: make(map[string]common.WorkbenchTask), Removed: make(map[string]bool), Hidden: make(map[string]bool)}
	s.volatileRevision.Add(1)
	s.wg.Add(1)
	go s.runWorkbenchConnector(ctx, cancel, store, process, commit)
	return legacyID, nil
}

func (s *Service) runWorkbenchConnector(ctx context.Context, cancel context.CancelFunc, store workbenchManagedConnectorStore,
	process *integrations.WorkbenchConnectorProcess, commit common.WorkbenchConnectorSyncCommit) {
	defer s.wg.Done()
	defer cancel()
	stage, err := readWorkbenchConnectorPages(ctx, store, process, &commit)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workbenchActive, commit.Scope.ScopeID)
	if err == nil && ctx.Err() != nil {
		stage, err = "publish", ctx.Err()
	}
	finished := time.Now().UTC()
	duration := finished.Sub(commit.Run.StartedAt).Milliseconds()
	commit.Run.FinishedAt, commit.Run.DurationMS = &finished, &duration
	if err == nil {
		commit.Run.Status = "success"
		if err = store.CommitWorkbenchConnectorSync(commit); err != nil {
			stage = "publish"
		}
	}
	if err != nil {
		commit.Run.Status = "failed"
		commit.Run.FailedStage, commit.Run.ErrorKind = stage, connectorErrorKind(err)
		switch {
		case errors.Is(err, context.Canceled):
			commit.Run.Status = "cancelled"
		case connectorErrorKind(err) == string(protocol.AccessDenied):
			commit.Run.Status = "access_denied"
		case connectorErrorKind(err) == string(protocol.AuthRequired):
			commit.Run.Status = "auth_required"
		case common.ProtocolError(err).Code == "STALE_REVISION":
			commit.Run.Status, commit.Run.ErrorKind = "superseded", "stale_revision"
		}
		var rpc *protocol.RPCError
		if errors.As(err, &rpc) {
			commit.Run.RetryAt = rpc.Data.RetryAt
		}
		if finishErr := store.FinishWorkbenchConnectorFailure(commit); finishErr != nil && common.ProtocolError(finishErr).Code != "STALE_REVISION" {
			log.Print("workbench: could not persist Connector sync failure")
		}
		if !s.stopping {
			switch {
			case commit.Run.Mode == "delta" && commit.Run.ErrorKind == string(protocol.InvalidCheckpoint):
				_, _ = s.startWorkbenchConnectorRunLocked(store, commit.Scope.ScopeID, "invalid_checkpoint", true)
			case commit.Run.Status == "superseded":
				scope, err := store.WorkbenchConnectorScope(commit.Scope.ScopeID)
				if err == nil && scope.AuthState == "authorized" && scope.AuthGeneration != commit.Scope.AuthGeneration {
					_, _ = s.startWorkbenchConnectorRunLocked(store, scope.ScopeID, "relogin", true)
				}
			}
		}
	}
	s.volatileRevision.Add(1)
}

func readWorkbenchConnectorPages(ctx context.Context, store workbenchManagedConnectorStore, process *integrations.WorkbenchConnectorProcess,
	c *common.WorkbenchConnectorSyncCommit) (string, error) {
	p := protocol.SyncParams{ScopeContext: c.Scope.Context, Mode: c.Run.Mode, Fields: []string{"assignees", "labels", "priority", "state_reason"}}
	if p.Mode == "delta" {
		p.Checkpoint = c.Scope.Checkpoint
	}
	seen := make(map[string]bool)
	versions := make(map[string]time.Time)
	for i := 0; i < connectorMaxPages; i++ {
		var counter connectorRunCounter
		page, err := callWorkbenchConnector[protocol.SyncPage](ctx, process, common.ID(), protocol.Sync, p, counter.notify)
		if err != nil {
			connectorFailedUsage(&c.Run, counter, err)
			return "read", err
		}
		if !validConnectorUsage(page.Usage) {
			return "read", integrations.ErrWorkbenchConnectorProtocol
		}
		connectorUsage(&c.Run, page.Usage)
		if page.Records == nil {
			return "read", integrations.ErrWorkbenchConnectorProtocol
		}
		for _, record := range page.Records {
			task, err := connectorTask(c.Scope, record)
			if err != nil {
				return "read", err
			}
			if previous, ok := versions[record.SourceID]; !ok || !record.UpdatedAt.Before(previous) {
				versions[record.SourceID] = record.UpdatedAt
				c.Records[record.SourceID] = task
			}
		}
		for _, removal := range page.Removals {
			if removal.SourceID == "" {
				return "read", integrations.ErrWorkbenchConnectorProtocol
			}
			switch removal.Status {
			case "access_denied":
				c.Hidden[removal.SourceID] = true
			case "deleted", "out_of_scope":
				c.Removed[removal.SourceID] = true
			default:
				return "read", integrations.ErrWorkbenchConnectorProtocol
			}
		}
		if page.NextPage == "" {
			if page.Coverage != p.Mode || page.NextCheckpoint == "" {
				return "read", integrations.ErrWorkbenchConnectorProtocol
			}
			c.Checkpoint = page.NextCheckpoint
			for key := range c.Removed {
				if _, exists := c.Records[key]; exists {
					return "read", integrations.ErrWorkbenchConnectorProtocol
				}
			}
			for key := range c.Hidden {
				delete(c.Records, key)
				c.Removed[key] = true
			}
			if p.Mode == "full" {
				return "resolve", resolveWorkbenchConnectorMissing(ctx, store, process, c, p.Fields)
			}
			return "", nil
		}
		if page.Coverage != "" || page.NextCheckpoint != "" || seen[page.NextPage] {
			return "read", integrations.ErrWorkbenchConnectorProtocol
		}
		seen[page.NextPage] = true
		p.Page = page.NextPage
	}
	return "read", integrations.ErrWorkbenchConnectorProtocol
}

func resolveWorkbenchConnectorMissing(ctx context.Context, store workbenchManagedConnectorStore, process *integrations.WorkbenchConnectorProcess,
	c *common.WorkbenchConnectorSyncCommit, fields []string) error {
	members, err := store.WorkbenchConnectorMembers(c.Scope.ScopeID)
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	numbers := make(map[string]string)
	for _, task := range members {
		if _, present := c.Records[task.SourceKey]; !present && !c.Removed[task.SourceKey] {
			missing = append(missing, task.SourceKey)
			numbers[task.SourceKey] = task.NativeID
		}
	}
	sort.Strings(missing)
	for i := 0; i < len(missing); i += 100 {
		ids := missing[i:min(i+100, len(missing))]
		request := protocol.ResolveParams{ScopeContext: c.Scope.Context, SourceIDs: ids, Fields: fields, KnownNumbers: make(map[string]string)}
		for _, id := range ids {
			request.KnownNumbers[id] = numbers[id]
		}
		var counter connectorRunCounter
		result, err := callWorkbenchConnector[protocol.ResolveResult](ctx, process, common.ID(), protocol.Resolve, request, counter.notify)
		if err != nil {
			connectorFailedUsage(&c.Run, counter, err)
			return err
		}
		if !validConnectorUsage(result.Usage) {
			return integrations.ErrWorkbenchConnectorProtocol
		}
		connectorUsage(&c.Run, result.Usage)
		unverified := false
		for _, item := range result.Items {
			if _, ok := request.KnownNumbers[item.SourceID]; !ok {
				return integrations.ErrWorkbenchConnectorProtocol
			}
			delete(request.KnownNumbers, item.SourceID)
			switch item.Status {
			case "present":
				if item.Record == nil || item.Record.SourceID != item.SourceID {
					return integrations.ErrWorkbenchConnectorProtocol
				}
				task, err := connectorTask(c.Scope, *item.Record)
				if err != nil {
					return err
				}
				c.Records[item.SourceID] = task
			case "deleted", "out_of_scope":
				c.Removed[item.SourceID] = true
			case "access_denied":
				c.Hidden[item.SourceID] = true
				c.Removed[item.SourceID] = true
			case "unverified":
				unverified = true
			default:
				return integrations.ErrWorkbenchConnectorProtocol
			}
		}
		if len(request.KnownNumbers) != 0 {
			return integrations.ErrWorkbenchConnectorProtocol
		}
		if unverified {
			return protocol.Problem(protocol.Temporary, "scope", "Scope membership could not be verified")
		}
	}
	return nil
}

func connectorTask(scope common.WorkbenchConnectorStoredScope, record protocol.SourceRecord) (common.WorkbenchTask, error) {
	if record.SourceID == "" || record.SourceVersion == "" || record.Title == "" || !validWorkbenchState(record.State) || record.UpdatedAt.IsZero() {
		return common.WorkbenchTask{}, integrations.ErrWorkbenchConnectorProtocol
	}
	locator := ""
	if record.URL != "" {
		if target, err := url.Parse(record.URL); err == nil && target.Scheme == "https" && target.Hostname() != "" && target.User == nil {
			locator = record.URL
		}
	}
	task := common.WorkbenchTask{ConnectionID: scope.ScopeID, Kind: scope.ConnectorID, Mode: "sync",
		SourceKey: record.SourceID, NativeID: record.Number, Title: record.Title, Locator: locator, State: record.State,
		NativeFields: map[string]string{"state": record.StateID, "state_label": record.StateLabel, "updated_at": record.UpdatedAt.UTC().Format(time.RFC3339Nano), "source_version": record.SourceVersion}}
	for id, field := range record.Fields {
		switch {
		case id == "labels" && field.Type == protocol.FieldLabels:
			if json.Unmarshal(field.Value, &task.Labels) != nil {
				return common.WorkbenchTask{}, integrations.ErrWorkbenchConnectorProtocol
			}
		case id == "assignees" && field.Type == protocol.FieldAccounts:
			var values []protocol.Option
			if json.Unmarshal(field.Value, &values) != nil {
				return common.WorkbenchTask{}, integrations.ErrWorkbenchConnectorProtocol
			}
			for _, value := range values {
				task.Assignees = append(task.Assignees, value.Label)
			}
		case field.Type == protocol.FieldText:
			var value string
			if json.Unmarshal(field.Value, &value) != nil {
				return common.WorkbenchTask{}, integrations.ErrWorkbenchConnectorProtocol
			}
			task.NativeFields["field."+id] = value
		}
	}
	return task, nil
}

func (s *Service) scheduleWorkbenchConnectorScope(scope common.WorkbenchSnapshot, trigger string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.workbenchActive[scope.ID] != nil {
		return
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return
	}
	last, err := store.WorkbenchConnectorLastRun(scope.ID)
	if err != nil {
		return
	}
	if trigger == "periodic" && last != nil && time.Since(last.StartedAt) < connectorDeltaInterval {
		return
	}
	_, _ = s.startWorkbenchConnectorRunLocked(store, scope.ID, trigger, false)
}
