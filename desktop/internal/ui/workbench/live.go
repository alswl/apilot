package workbench

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo"
)

func (m *mockup) startLiveEvents() {
	if m.live == nil || m.win == nil {
		return
	}
	m.liveStale = m.liveLoaded
	m.liveGeneration++
	remote, window := m.live, m.win
	ctx, cancel := context.WithCancel(context.Background())
	window.OnClosed(func() {
		cancel()
		m.stopPermissionRetry()
	})
	current := func() bool {
		return ctx.Err() == nil && m.win == window && !window.IsDestroyed()
	}
	go func() {
		for ctx.Err() == nil {
			_ = remote.WatchWorkbenchEvents(ctx, func(event common.WorkbenchEvent) error {
				mygo.RunOnMain(func() {
					if current() {
						m.receiveLiveEvent(event)
					}
				})
				return ctx.Err()
			})
			mygo.RunOnMain(func() {
				if current() {
					m.receiveLiveDisconnect()
					window.Invalidate()
				}
			})
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
			timer.Stop()
		}
	}()
}

func (m *mockup) receiveLiveDisconnect() {
	m.stopPermissionRetry()
	m.liveDisconnected, m.liveStale = true, true
	if !m.liveLoaded {
		m.liveError = true
	}
}

func (m *mockup) receiveLiveEvent(event common.WorkbenchEvent) {
	if event.Type == "snapshot" {
		m.liveDisconnected = false
		m.livePermissionRetries = 0
		m.liveGeneration++
	}
	m.liveAnnotationsDirty = true
	if panel := m.connectorRuns; panel != nil && !panel.loading {
		if panel.detail != nil && panel.detail.Status == "running" {
			m.inspectConnectorRun(panel, panel.detail.ID)
		} else {
			m.loadConnectorRuns(panel)
		}
	}
	if m.page == usagePage && m.usageTab == 2 && m.connectorDesktop() {
		m.loadConnectorUsage()
	}
	m.refreshLive()
}

func (m *mockup) runLiveTaskWrite(request common.WorkbenchTaskWrite, finish func(common.WorkbenchTaskReceipt, error)) {
	remote := m.live
	go func() {
		receipt, err := remote.SubmitWorkbenchTaskOperation(request)
		mygo.RunOnMain(func() {
			if request.SourceObjectID == "" {
				if m.newTaskRequest.RequestID == request.RequestID {
					finish(receipt, err)
				}
			} else if m.liveTaskRequests[request.SourceObjectID].RequestID == request.RequestID {
				finish(receipt, err)
			}
			m.liveGeneration++
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
				m.refreshLive()
			}
		})
	}()
}

func (m *mockup) refreshLive() {
	m.refreshLiveWithDispatch(mygo.RunOnMain)
}

func (m *mockup) refreshLiveWithDispatch(dispatch func(func())) {
	if m.devEvents != nil {
		m.devEvents.read(m)
		return
	}
	remote := m.live
	if remote == nil {
		return
	}
	if m.liveBusy {
		m.liveQueued = true
		return
	}
	m.stopPermissionRetry()
	m.liveBusy, m.liveQueued = true, false
	generation := m.liveGeneration
	managedDesktop := m.connectorDesktop()
	fixtureEnabled := m.fixtureConnector
	syncID := m.syncLogID
	requests := make(map[string]string)
	for taskID, request := range m.liveTaskRequests {
		if m.pendingTaskEdits[taskID] && request.RequestID != "" {
			requests[taskID] = request.RequestID
		}
	}
	createRequest := ""
	if m.newTaskPending {
		createRequest = m.newTaskRequest.RequestID
	}
	confirm := func(snapshots []common.WorkbenchSnapshot) {
		confirmed := slices.Clone(snapshots)
		dispatch(func() {
			// Connection lists are observed by one refresh worker in request order.
			// A local UI generation change only invalidates the aggregate result.
			if m.clearChangedGitHubAccess(confirmed) && m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
			}
		})
	}
	go func() {
		connections, err := confirmLiveConnections(remote.WorkbenchConnections, confirm)
		detailAccessChanged := false
		readDetail := func(id string) (common.WorkbenchSnapshot, error) {
			snapshot, err := remote.WorkbenchSyncDetails(id)
			if err != nil || snapshot.ID != id || snapshot.Kind != common.WorkbenchGitHubRepository ||
				matchesLiveGitHubAccess(connections, snapshot) {
				return snapshot, err
			}
			index := slices.IndexFunc(connections, func(connection common.WorkbenchSnapshot) bool {
				return connection.ID == id
			})
			switch {
			case index >= 0 && snapshot.Version < connections[index].Version:
				return common.WorkbenchSnapshot{}, nil
			case index < 0 && snapshot.AccessState == "available":
				return common.WorkbenchSnapshot{}, nil
			}
			detailAccessChanged = true
			dispatch(func() {
				m.clearGitHubAccess(map[string]bool{id: true})
				if m.win != nil && !m.win.IsDestroyed() {
					m.win.Invalidate()
				}
			})
			return snapshot, nil
		}
		var detail common.WorkbenchSnapshot
		var detailErr error
		if err == nil {
			for i := range connections {
				if connections[i].Kind == common.WorkbenchLocal {
					continue
				}
				var snapshot common.WorkbenchSnapshot
				snapshot, err = readDetail(connections[i].ID)
				if err != nil {
					break
				}
				connections[i].PendingWrites = snapshot.PendingWrites
				if connections[i].ParentConnectionID != "" {
					connections[i].Attempts = snapshot.Attempts
				}
				if connections[i].ID == syncID {
					detail = snapshot
				}
			}
		}
		var parents []common.WorkbenchConnectorOverview
		if err == nil && managedDesktop {
			var result connectorItems[common.WorkbenchConnectorOverview]
			result, err = connectorCall[connectorItems[common.WorkbenchConnectorOverview]](remote, "workbench.connector.connection.list", nil)
			if err == nil {
				parents = slices.DeleteFunc(result.Items, func(parent common.WorkbenchConnectorOverview) bool {
					return parent.Connection.ConnectorID == managedFixture && !fixtureEnabled
				})
			}
		}
		var plugins []services.WorkbenchPlugin
		if err == nil {
			plugins, err = remote.WorkbenchPlugins()
		}
		var tasks []sourceObject
		revalidate := false
		if err == nil {
			q := common.WorkbenchQuery{Limit: 100}
			for {
				page, readErr := remote.WorkbenchTasks(q)
				if readErr != nil {
					err = readErr
					break
				}
				tasks = append(tasks, page.Items...)
				if len(tasks) >= page.Total {
					break
				}
				q.Offset, q.Revision = len(tasks), page.Revision
			}
		}
		if err == nil {
			// The task pages and connection list are separate requests. Reject a
			// page captured across an account or authorization transition.
			var latest []common.WorkbenchSnapshot
			latest, err = confirmLiveConnections(remote.WorkbenchConnections, confirm)
			if err == nil {
				changed := changedGitHubVisibility(connections, latest)
				if len(changed) != 0 {
					revalidate = true
					connections = latest
					tasks = slices.DeleteFunc(tasks, func(task sourceObject) bool { return changed[task.ConnectionID] })
					if changed[syncID] {
						detail, detailErr = common.WorkbenchSnapshot{}, nil
					}
				}
			}
		}
		if syncID != "" && detail.ID == "" {
			detail, detailErr = readDetail(syncID)
		}
		if detail.Kind == common.WorkbenchGitHubRepository && !matchesLiveGitHubAccess(connections, detail) {
			detail = common.WorkbenchSnapshot{}
		}
		receipts := make(map[string]common.WorkbenchTaskReceipt, len(requests))
		for taskID, requestID := range requests {
			if receipt, receiptErr := remote.WorkbenchTaskOperationReceipt(requestID); receiptErr == nil {
				receipts[taskID] = receipt
			}
		}
		var createReceipt common.WorkbenchTaskReceipt
		if createRequest != "" {
			createReceipt, _ = remote.WorkbenchTaskOperationReceipt(createRequest)
		}
		dispatch(func() {
			m.liveBusy = false
			if m.win == nil || m.win.IsDestroyed() {
				return
			}
			defer func() {
				if m.liveQueued {
					m.refreshLive()
				}
			}()
			if generation != m.liveGeneration {
				m.liveQueued = true
				return
			}
			if detailAccessChanged {
				m.liveStale = true
				m.win.Invalidate()
				if !m.liveQueued {
					m.schedulePermissionRetry(dispatch)
				}
				return
			}
			settled := false
			for taskID, receipt := range receipts {
				if m.pendingTaskEdits[taskID] && m.liveTaskRequests[taskID].RequestID == receipt.RequestID && !workbenchOperationInProgress(receipt.SyncState) {
					m.finishLiveTaskToggle(taskID, receipt, nil)
					settled = true
				}
			}
			if m.newTaskPending && createRequest != "" && m.newTaskRequest.RequestID == createReceipt.RequestID && !workbenchOperationInProgress(createReceipt.SyncState) {
				m.finishLiveTaskCreate(createReceipt, nil)
				settled = true
			}
			detailChanged := false
			if syncID != "" && syncID == m.syncLogID && detailErr == nil {
				detailChanged = !reflect.DeepEqual(detail, m.syncDetail)
				m.syncDetail = detail
			}
			annotationsChanged := false
			if err == nil && !m.liveQueued && m.liveAnnotationsDirty {
				annotationsChanged = m.invalidateLiveAnnotations()
				m.liveAnnotationsDirty = false
			}
			hadError, wasStale := m.liveError, m.liveStale
			m.liveError = err != nil
			m.liveStale = m.liveDisconnected || err != nil || revalidate
			if err == nil && (!m.liveLoaded || !reflect.DeepEqual(connections, m.liveConnections) || !reflect.DeepEqual(plugins, m.livePlugins) || !reflect.DeepEqual(parents, m.connectorParents) || !reflect.DeepEqual(tasks, m.objects)) {
				m.liveConnections = connections
				m.livePlugins = plugins
				m.connectorParents = parents
				m.applyLive(connections, tasks)
				m.win.Invalidate()
			} else if hadError != m.liveError || wasStale != m.liveStale || detailChanged || settled || annotationsChanged {
				m.win.Invalidate()
			}
			if settled && !revalidate {
				m.liveQueued = true
			}
			switch {
			case revalidate || (err != nil && m.livePermissionRetries > 0):
				if !m.liveQueued {
					m.schedulePermissionRetry(dispatch)
				}
				return
			case err == nil:
				m.livePermissionRetries = 0
			}
			if err != nil && !m.liveQueued && !m.liveDisconnected {
				window := m.win
				time.AfterFunc(time.Second, func() {
					dispatch(func() {
						if m.win == window && !window.IsDestroyed() && m.liveError && !m.liveDisconnected && generation == m.liveGeneration {
							m.refreshLive()
						}
					})
				})
			}
		})
	}()
}

const (
	livePermissionRetryLimit = 3
	livePermissionRetryDelay = 750 * time.Millisecond
)

func (m *mockup) nextPermissionRetry() (time.Duration, bool) {
	if m.liveDisconnected || m.livePermissionRetries >= livePermissionRetryLimit {
		return 0, false
	}
	m.livePermissionRetries++
	return livePermissionRetryDelay, true
}

func (m *mockup) stopPermissionRetry() {
	if m.livePermissionTimer != nil {
		m.livePermissionTimer.Stop()
		m.livePermissionTimer = nil
	}
}

func (m *mockup) schedulePermissionRetry(dispatch func(func())) {
	if m.livePermissionTimer != nil {
		return
	}
	delay, ok := m.nextPermissionRetry()
	if !ok {
		return
	}
	window := m.win
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		dispatch(func() {
			if m.livePermissionTimer != timer {
				return
			}
			m.livePermissionTimer = nil
			if m.win == window && !window.IsDestroyed() && !m.liveDisconnected {
				m.refreshLiveWithDispatch(dispatch)
			}
		})
	})
	m.livePermissionTimer = timer
}

func confirmLiveConnections(read func() ([]common.WorkbenchSnapshot, error), confirm func([]common.WorkbenchSnapshot)) ([]common.WorkbenchSnapshot, error) {
	snapshots, err := read()
	if err == nil {
		confirm(snapshots)
	}
	return snapshots, err
}

func matchesLiveGitHubAccess(connections []common.WorkbenchSnapshot, detail common.WorkbenchSnapshot) bool {
	for _, connection := range connections {
		if connection.ID == detail.ID {
			return connection.Kind == detail.Kind && connection.Version == detail.Version &&
				connection.AccessState == detail.AccessState && connection.AccountLogin == detail.AccountLogin
		}
	}
	return false
}

func sameGitHubVisibility(before, after []common.WorkbenchSnapshot) bool {
	return len(changedGitHubVisibility(before, after)) == 0
}

func changedGitHubVisibility(before, after []common.WorkbenchSnapshot) map[string]bool {
	type visibility struct {
		version       int64
		access, login string
	}
	states := func(snapshots []common.WorkbenchSnapshot) map[string]visibility {
		result := make(map[string]visibility)
		for _, snapshot := range snapshots {
			if snapshot.Kind == common.WorkbenchGitHubRepository || snapshot.ParentConnectionID != "" {
				result[snapshot.ID] = visibility{snapshot.Version, snapshot.AccessState, snapshot.AccountLogin}
			}
		}
		return result
	}
	previous, current := states(before), states(after)
	changed := make(map[string]bool)
	for id, state := range previous {
		if current[id] != state {
			changed[id] = true
		}
	}
	for id, state := range current {
		if previous[id] != state {
			changed[id] = true
		}
	}
	return changed
}

func (m *mockup) clearChangedGitHubAccess(snapshots []common.WorkbenchSnapshot) bool {
	changed := changedGitHubVisibility(m.liveConnections, snapshots)
	if len(changed) == 0 {
		return false
	}
	m.clearGitHubAccess(changed)
	return true
}

func (m *mockup) clearGitHubAccess(changed map[string]bool) {
	for id := range changed {
		m.clearGitHubTaskDrafts(id)
		delete(m.syncLogs, id)
		delete(m.pendingSync, id)
		delete(m.syncPhase, id)
		delete(m.syncAttempts, id)
		if m.syncDetail.ID == id || m.syncLogID == id {
			m.syncDetailProblem = ""
		}
		if m.syncDetail.ID == id {
			for _, receipt := range m.syncDetail.PendingWrites {
				delete(m.checkingOperations, receipt.RequestID)
			}
			m.syncDetail = common.WorkbenchSnapshot{}
		}
	}
	for i := range m.liveConnections {
		if snapshot := &m.liveConnections[i]; changed[snapshot.ID] {
			snapshot.Attempts = nil
			snapshot.PendingWrites = nil
			snapshot.LastSuccess = nil
		}
	}
	for i := range m.connectionsData {
		conn := &m.connectionsData[i]
		if changed[conn.ID] {
			conn.Identity, conn.Writable = "", false
			conn.ActiveAttempt, conn.ReadProgress = 0, nil
			conn.LastConfirmedAt, conn.RateLimitUntil = "—", nil
			conn.Status, conn.Capabilities = "授权不可用", "只读"
		}
	}
	for id, receipt := range m.serverTaskOperations {
		if changed[receipt.ConnectionID] {
			delete(m.serverTaskOperations, id)
		}
	}
	tasks := slices.DeleteFunc(slices.Clone(m.objects), func(task sourceObject) bool { return changed[task.ConnectionID] })
	m.replaceLiveTasks(tasks)
}

func workbenchOperationInProgress(state string) bool {
	return state == common.WorkbenchOperationAccepted || state == common.WorkbenchOperationDispatching
}

func (m *mockup) applyLive(snapshots []common.WorkbenchSnapshot, tasks []sourceObject) {
	m.liveLoaded = true
	if m.connectorDesktop() {
		snapshots = slices.DeleteFunc(slices.Clone(snapshots), func(snapshot common.WorkbenchSnapshot) bool { return !m.visibleConnectorKind(snapshot.Kind) })
		tasks = slices.DeleteFunc(slices.Clone(tasks), func(task sourceObject) bool { return !m.visibleConnectorKind(task.Kind) })
	}
	sourceName, sourceID := m.taskViewSource()
	visibleGitHub := make(map[string]bool)
	for _, snapshot := range snapshots {
		if snapshot.Kind != common.WorkbenchGitHubRepository && !m.isManagedConnector(snapshot.Kind) {
			continue
		}
		previous := m.connectionByID(snapshot.ID)
		visibleGitHub[snapshot.ID] = snapshot.AccessState == "available"
		if snapshot.AccessState != "available" || (previous != nil && previous.Version != snapshot.Version) {
			m.clearGitHubTaskDrafts(snapshot.ID)
		}
	}
	for _, previous := range m.connectionsData {
		if previous.SourceKind == common.WorkbenchGitHubRepository || m.isManagedConnector(previous.SourceKind) {
			if _, present := visibleGitHub[previous.ID]; !present {
				m.clearGitHubTaskDrafts(previous.ID)
			}
		}
	}
	tasks = slices.DeleteFunc(tasks, func(task sourceObject) bool {
		return (task.Kind == common.WorkbenchGitHubRepository || m.isManagedConnector(task.Kind)) && !visibleGitHub[task.ConnectionID]
	})
	m.serverTaskOperations = make(map[string]common.WorkbenchTaskReceipt)
	for _, snapshot := range snapshots {
		for _, receipt := range snapshot.PendingWrites {
			if receipt.SourceObjectID == "" || (!workbenchOperationInProgress(receipt.SyncState) && receipt.SyncState != common.WorkbenchOperationNeedsReview) {
				continue
			}
			if _, exists := m.serverTaskOperations[receipt.SourceObjectID]; !exists {
				m.serverTaskOperations[receipt.SourceObjectID] = receipt
			}
		}
	}
	if len(m.pendingTaskEdits) > 0 {
		current := make(map[string]sourceObject, len(m.pendingTaskEdits))
		visibleTasks := make(map[string]bool, len(tasks))
		for _, task := range tasks {
			visibleTasks[task.ID] = true
		}
		for _, task := range m.objects {
			if m.pendingTaskEdits[task.ID] && ((task.Kind != common.WorkbenchGitHubRepository && !m.isManagedConnector(task.Kind)) || visibleTasks[task.ID]) {
				current[task.ID] = task
			}
		}
		for i, task := range tasks {
			if previous, ok := current[task.ID]; ok {
				tasks[i] = previous
				delete(current, task.ID)
			}
		}
		for _, task := range m.objects {
			if _, missing := current[task.ID]; missing {
				tasks = append(tasks, task)
			}
		}
	}
	var connections []connection
	kinds := services.WorkbenchConnectors()
	kinds = append(kinds,
		services.WorkbenchConnector{Kind: managedGitHub, Name: connectorLabel(managedGitHub), SearchAliases: []string{"github", "issues", "repo"}},
		services.WorkbenchConnector{Kind: managedFixture, Name: connectorLabel(managedFixture)},
	)
	for _, plugin := range m.livePlugins {
		if plugin.Release != nil && !slices.ContainsFunc(kinds, func(kind services.WorkbenchConnector) bool { return kind.Kind == plugin.Release.ConnectorID }) {
			id := plugin.Release.ConnectorID
			kinds = append(kinds, services.WorkbenchConnector{Kind: id, Name: connectorLabel(id)})
		}
	}
	for _, parent := range m.connectorParents {
		id := parent.Connection.ConnectorID
		if !slices.ContainsFunc(kinds, func(kind services.WorkbenchConnector) bool { return kind.Kind == id }) {
			kinds = append(kinds, services.WorkbenchConnector{Kind: id, Name: connectorLabel(id)})
		}
	}
	for _, snapshot := range snapshots {
		for _, kind := range kinds {
			if kind.Kind != snapshot.Kind {
				continue
			}
			draft := m.connectorDraft(kind)
			draft.ID, draft.Name, draft.Version = snapshot.ID, snapshot.Name, snapshot.Version
			draft.Paused = snapshot.Paused
			draft.PluginDisabled = snapshot.PluginDisabled
			draft.WriteEnabled = snapshot.WriteEnabled
			draft.RateLimitUntil = snapshot.RateLimitUntil
			if existing := m.connectionByID(draft.ID); existing != nil && existing.Name != draft.Name && m.sourceFilter == existing.Name && (m.sourceFilterID == "" || m.sourceFilterID == draft.ID) {
				m.sourceFilter = draft.Name
			}
			draft.Scope, draft.VaultPath, draft.Repository = snapshot.Scope, snapshot.Vault, snapshot.Repository
			draft.Status, draft.Capabilities, draft.Identity = "等待首次同步", "只读", "本地"
			if (snapshot.Kind == common.WorkbenchObsidianTasks || snapshot.Kind == common.WorkbenchGitHubRepository) && snapshot.Capabilities.Create {
				draft.Writable = true
				draft.Capabilities = "读写"
			}
			if snapshot.Kind == common.WorkbenchGitHubRepository {
				draft.Scope, draft.Identity = snapshot.Repository, snapshot.AccountLogin
				if snapshot.WriteDenied {
					draft.Capabilities = "只读 · 回写被拒绝"
				}
			}
			if m.isManagedConnector(snapshot.Kind) {
				draft.Scope, draft.Repository, draft.Identity = snapshot.ScopeRef, snapshot.Name, snapshot.AccountLogin
				draft.Writable = snapshot.Capabilities.Create
				if snapshot.WriteEnabled {
					draft.Capabilities = "读写"
				}
			}
			if snapshot.Kind == common.WorkbenchLocal {
				draft.Scope, draft.Capabilities = "由 Apilot 管理", "读写"
			}
			if snapshot.Mode == "mock" {
				draft.Scope, draft.Identity = snapshot.Repository, "模拟数据"
			}
			draft.CheckboxMapping = map[string]string{"进行中": "", "不计划处理": ""}
			for symbol, state := range snapshot.Statuses {
				draft.CheckboxMapping[workbenchStateLabel(state)] = symbol
			}
			showSync := snapshot.Kind != common.WorkbenchGitHubRepository || snapshot.AccessState != "unavailable"
			if showSync && snapshot.LastSuccess != nil {
				draft.LastConfirmedAt = snapshot.LastSuccess.Local().Format(time.DateTime)
			}
			delete(m.pendingSync, draft.ID)
			if showSync && len(snapshot.Attempts) > 0 {
				switch snapshot.Attempts[0].State {
				case "running":
					draft.ActiveAttempt = snapshot.Attempts[0].ID
					draft.ReadProgress = snapshot.Attempts[0].Progress
					m.pendingSync[draft.ID] = true
				case "success":
					draft.Status = "同步正常"
				case "failed":
					draft.Status = "同步失败"
				default:
					if snapshot.LastSuccess != nil {
						draft.Status = "同步正常"
					}
				}
			}
			if snapshot.Kind == common.WorkbenchLocal {
				draft.Status = "可用"
				draft.Writable = snapshot.Capabilities.Create
			}
			if snapshot.Kind == common.WorkbenchGitHubRepository {
				switch snapshot.AccessState {
				case "pending":
					draft.Status = "等待授权确认"
				case "unavailable":
					draft.Status = "授权不可用"
				}
			}
			if m.isManagedConnector(snapshot.Kind) {
				switch snapshot.AccessState {
				case "available":
					if len(snapshot.Attempts) == 0 || snapshot.Attempts[0].State == "success" {
						draft.Status = "同步正常"
					}
				case "auth_required", "access_denied":
					draft.Status = "授权不可用"
					draft.Writable = false
				}
			}
			if draft.Paused {
				draft.Status = "已暂停"
			}
			if draft.PluginDisabled {
				draft.Status = "插件已停用"
				draft.Writable = false
			}
			events := make([]syncEvent, 0, len(snapshot.Attempts))
			for _, a := range slices.Backward(snapshot.Attempts) {
				message := tr(workbenchAttemptLabel(a.State))
				if a.SyncType == "full" {
					message += " · " + tr("全量同步")
				}
				if a.SyncType == "incremental" {
					message += " · " + tr("增量同步")
				}
				if a.RequestCount > 0 {
					message += fmt.Sprintf(tr(" · %d 次请求 / %d 页"), a.RequestCount, a.PageCount)
				}
				if a.Trigger == "import" {
					message = tr("导入完成")
				}
				if a.State == "success" {
					message += fmt.Sprintf(" (+%d ~%d −%d)", a.Added, a.Updated, a.Removed)
				}
				if a.Error != "" {
					message += " · " + a.Error
				}
				events = append(events, syncEvent{a.Started.Local().Format(time.DateTime), message})
			}
			if snapshot.Kind == common.WorkbenchGitHubRepository && snapshot.AccessState != "available" {
				delete(m.syncLogs, draft.ID)
			} else {
				m.syncLogs[draft.ID] = events
			}
			connections = append(connections, draft)
		}
	}
	m.connectionsData = connections
	m.sourceFilter = m.resolveTaskViewSource(sourceName, sourceID)
	m.replaceLiveTasks(tasks)
	for _, snapshot := range snapshots {
		recovery, tracking := m.connectorConflictRefreshes[snapshot.ID]
		if !tracking || recovery.attemptID == 0 || len(snapshot.Attempts) == 0 {
			continue
		}
		attempt := snapshot.Attempts[0]
		if attempt.ID < recovery.attemptID || attempt.Finished == nil {
			continue
		}
		delete(m.connectorConflictRefreshes, snapshot.ID)
		if attempt.State != "success" {
			m.liveTaskProblems[recovery.taskID] = "同步失败"
			continue
		}
		delete(m.connectorConflicts, recovery.taskID)
		if recovery.edit && m.page == tasksPage && m.taskSelection == recovery.taskID {
			for _, task := range m.objects {
				if task.ID == recovery.taskID {
					m.openConnectorTaskEditor(task, recovery.actionMode)
					break
				}
			}
		}
	}
}

func (m *mockup) clearGitHubTaskDrafts(connectionID string) {
	for _, task := range m.objects {
		if task.ConnectionID != connectionID {
			continue
		}
		delete(m.pendingTaskEdits, task.ID)
		delete(m.liveTaskRequests, task.ID)
		delete(m.liveTaskProblems, task.ID)
		delete(m.connectorConflicts, task.ID)
		if m.connectorTaskEditor != nil && m.connectorTaskEditor.task.ID == task.ID {
			m.connectorTaskEditor = nil
		}
		if m.connectorMapping != nil && m.connectorMapping.scope == connectionID {
			m.connectorMapping = nil
		}
		delete(m.checkingOperations, task.ID)
		delete(m.taskDetails, task.ID)
		delete(m.assistantThreads, task.ID)
		if m.sourceEditor != nil && m.sourceEditor.id == task.ID {
			m.sourceEditor = nil
		}
		if m.labelEditor != nil && m.labelEditor.taskID == task.ID {
			m.labelEditor = nil
		}
		if m.mutation.SourceObject == task.ID {
			m.mutation = mutation{}
		}
	}
	if m.newTaskSourceID == connectionID || m.newTaskRequest.ConnectionID == connectionID {
		m.newTaskRequest = common.WorkbenchTaskWrite{}
		m.newTaskTargets = taskDestinations{}
		m.newTaskSource, m.newTaskSourceID = "", ""
		m.newTaskDraft, m.newTaskBody = "", ""
		m.newTaskPending, m.newTaskOpen = false, false
	}
}

func (m *mockup) replaceLiveTasks(tasks []sourceObject) {
	selectedID := m.taskSelection
	previousPosition := -1
	if m.page == tasksPage && !m.fullTask && selectedID != "" {
		for i, index := range m.taskRows() {
			if m.objects[index].ID == selectedID {
				previousPosition = i
				break
			}
		}
	}
	if m.inspector && m.selected >= 0 && m.selected < len(m.objects) {
		selectedID = m.objects[m.selected].ID
	}
	m.objects = tasks
	if previousPosition >= 0 {
		rows := m.taskRows()
		stillVisible := slices.ContainsFunc(rows, func(index int) bool { return m.objects[index].ID == selectedID })
		if !stillVisible && len(rows) > 0 {
			next := rows[min(previousPosition, len(rows)-1)]
			m.taskSelection = m.objects[next].ID
			selectedID = m.taskSelection
			m.restoreTaskFocus = true
		}
	}
	if m.inspector {
		m.selected = slices.IndexFunc(tasks, func(task sourceObject) bool { return task.ID == selectedID })
		if m.selected < 0 {
			m.closeInspector()
		}
	}
	if m.taskSelection != "" && !slices.ContainsFunc(tasks, func(t sourceObject) bool { return t.ID == m.taskSelection }) {
		m.taskSelection = ""
		m.closeInspector()
	}
}

func (m *mockup) loadGitHubRepositories(edit *connectorEdit, authorize bool) {
	if edit.githubListing {
		return
	}
	edit.githubListing, edit.problem = true, ""
	remote := m.live
	id := ""
	if edit.githubAuthorization.State == "authorized" {
		id = edit.githubAuthorization.ID
	}
	go func() {
		repositories, err := remote.ListGitHubRepositories(id)
		mygo.RunOnMain(func() {
			if m.connectorEditor != edit {
				return
			}
			edit.githubListing = false
			if err == nil {
				edit.githubRepositories = repositories
				if !slices.Contains(repositories, edit.Repository) {
					edit.Repository = ""
				}
				edit.githubNeedsInstall = len(repositories) == 0
			} else if problem := common.ProtocolError(err); problem.Code == "UNAUTHORIZED" {
				edit.githubRepositories, edit.Repository = nil, ""
				edit.githubNeedsInstall = false
				if id != "" {
					m.cancelGitHubAuthorization(edit)
					edit.problem = "GitHub 授权未完成"
				} else if authorize {
					m.startGitHubAuthorization(edit)
				}
			} else {
				edit.problem = common.ProtocolError(err).Message
			}
			m.invalidateLiveWindow()
		})
	}()
}

func (m *mockup) continueGitHubSetup(edit *connectorEdit) {
	if edit.ID == "" {
		m.loadGitHubRepositories(edit, true)
	} else {
		m.saveLiveGitHubConnection(edit)
	}
}

func (m *mockup) saveLiveGitHubConnection(edit *connectorEdit) {
	if edit.saving {
		return
	}
	config, remote, authorization := edit.connection, m.live, edit.githubAuthorization
	if config.ID == "" && (config.Name == "" || config.Name == "GitHub") {
		config.Name = strings.TrimSpace(config.Repository)
	}
	c := common.WorkbenchConnection{ID: config.ID, Version: config.Version, Kind: config.SourceKind, Name: config.Name, Repository: config.Repository}
	edit.saving, edit.problem, edit.duplicateID = true, "", ""
	go func() {
		var snapshot common.WorkbenchSnapshot
		var err error
		if authorization.State == "authorized" {
			snapshot, err = remote.ConfigureWorkbenchGitHubAuthorized(c, authorization.ID, config.WriteEnabled)
		} else {
			snapshot, err = remote.ConfigureWorkbenchGitHub(c, "", config.WriteEnabled)
		}
		mygo.RunOnMain(func() {
			if m.connectorEditor == edit {
				edit.saving = false
				if err != nil {
					problem := common.ProtocolError(err)
					edit.problem = problem.Message
					edit.duplicateID, _ = problem.Details["connection_id"].(string)
					edit.githubNeedsInstall, _ = problem.Details["github_installation_required"].(bool)
					if problem.Code == "UNAUTHORIZED" && authorization.State != "authorized" {
						m.startGitHubAuthorization(edit)
					} else if authorization.State == "authorized" && (problem.Code == "UNAUTHORIZED" || problem.Code == "NOT_FOUND" || problem.Code == "CONFLICT" && edit.duplicateID == "") {
						m.cancelGitHubAuthorization(edit)
					}
				} else {
					if edit.githubSetupCancel != nil {
						edit.githubSetupCancel()
					}
					m.syncLogRestoreFocus = snapshot.ID
					m.connectorEditor = nil
				}
			}
			m.liveGeneration++
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
				m.refreshLive()
			}
		})
	}()
}

func (m *mockup) startGitHubAuthorization(edit *connectorEdit) {
	if edit.githubAuthorizing || m.live == nil {
		return
	}
	edit.githubAuthorizing, edit.problem = true, ""
	remote := m.live
	go func() {
		status, err := remote.StartGitHubAuthorization()
		mygo.RunOnMain(func() {
			if m.connectorEditor != edit {
				if err == nil {
					go func() { _ = remote.CancelGitHubAuthorization(status.ID) }()
				}
				return
			}
			edit.githubAuthorizing = false
			if err != nil {
				edit.problem = common.ProtocolError(err).Message
				m.invalidateLiveWindow()
				return
			}
			edit.githubAuthorization = status
			m.openGitHubVerification(edit)
			go m.watchGitHubAuthorization(edit, remote, status.ID)
		})
	}()
}

func (m *mockup) openGitHubVerification(edit *connectorEdit) {
	mygo.Clipboard.WriteText(edit.githubAuthorization.UserCode)
	edit.problem = ""
	if err := mygo.Shell.OpenExternal(edit.githubAuthorization.VerificationURI); err != nil {
		edit.problem = "无法打开 GitHub"
	}
	m.invalidateLiveWindow()
}

func (m *mockup) watchGitHubAuthorization(edit *connectorEdit, remote *services.Remote, id string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		status, err := remote.GitHubAuthorizationStatus(id)
		mygo.RunOnMain(func() {
			if m.connectorEditor != edit || edit.githubAuthorization.ID != id {
				return
			}
			if err != nil {
				edit.githubAuthorization = services.GitHubAuthorizationStatus{}
				edit.problem = common.ProtocolError(err).Message
				go func() { _ = remote.CancelGitHubAuthorization(id) }()
			} else {
				edit.githubAuthorization = status
				if status.State == "authorized" {
					m.continueGitHubSetup(edit)
					m.focusGitHubConnection()
				}
				if status.State == "denied" || status.State == "expired" || status.State == "failed" {
					edit.problem = "GitHub 授权未完成"
					go func() { _ = remote.CancelGitHubAuthorization(id) }()
				}
			}
			m.invalidateLiveWindow()
		})
		if err != nil || status.State != "waiting" {
			return
		}
	}
}

func (m *mockup) focusGitHubConnection() {
	if m.win != nil && !m.win.IsDestroyed() {
		mygo.App.Focus()
		m.win.Focus()
	}
}

func (m *mockup) cancelGitHubAuthorization(edit *connectorEdit) {
	id, remote := edit.githubAuthorization.ID, m.live
	edit.githubAuthorization = services.GitHubAuthorizationStatus{}
	if id != "" && remote != nil {
		go func() { _ = remote.CancelGitHubAuthorization(id) }()
	}
}

func (m *mockup) invalidateLiveWindow() {
	if m.win != nil && !m.win.IsDestroyed() {
		m.win.Invalidate()
	}
}

func (m *mockup) saveLiveConnection(config connection, bridge common.WorkbenchBridgeInput) string {
	c := common.WorkbenchConnection{ID: config.ID, Version: config.Version, Kind: config.SourceKind, Name: config.Name, Vault: config.VaultPath, Scope: config.Scope, Repository: config.Repository}
	if c.Kind == githubIssues {
		c.Repository = "alswl/issues"
	} else {
		c.Statuses = map[string]string{}
		for _, state := range config.stateMappings() {
			normalized := map[string]string{"进行中": "in_progress", "不计划处理": "cancelled"}[state.label]
			if normalized == "" || state.native == "" {
				continue
			}
			if _, exists := c.Statuses[state.native]; exists {
				return "状态映射必须使用互不重复的单个字符"
			}
			c.Statuses[state.native] = normalized
		}
	}
	snapshot, err := m.live.ConfigureWorkbench(c, bridge)
	if err != nil {
		return common.ProtocolError(err).Message
	}
	m.liveGeneration++
	if snapshot.LastSuccess == nil || (config.ID != "" && snapshot.Version != config.Version) {
		visible := slices.DeleteFunc(slices.Clone(m.objects), func(task sourceObject) bool { return task.ConnectionID == snapshot.ID })
		m.replaceLiveTasks(visible)
	}
	m.refreshLive()
	return ""
}

func workbenchAttemptLabel(state string) string {
	switch state {
	case "running":
		return "读取来源"
	case "success":
		return "同步完成"
	case "cancelled", "interrupted", "superseded":
		return "同步中断"
	default:
		return "同步失败"
	}
}

func workbenchStateLabel(state string) string {
	switch state {
	case "open":
		return "待处理"
	case "in_progress":
		return "进行中"
	case "done":
		return "已完成"
	case "cancelled":
		return "不计划处理"
	default:
		return "未知"
	}
}

func (m *mockup) sourceText(value string) string {
	if m.live != nil {
		return value
	}
	return tr(value)
}
