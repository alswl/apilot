package workbench

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
	"github.com/egoist/mygo"
)

func (m *mockup) pollLive() {
	if m.live == nil {
		return
	}
	m.refreshLive()
	window := m.win
	time.AfterFunc(time.Second, func() {
		mygo.RunOnMain(func() {
			if m.win == window && window != nil && !window.IsDestroyed() {
				m.pollLive()
			}
		})
	})
}

func (m *mockup) refreshLive() {
	remote := m.live
	if remote == nil || m.liveBusy {
		return
	}
	m.liveBusy = true
	generation := m.liveGeneration
	syncID := m.syncLogID
	go func() {
		connections, err := remote.WorkbenchConnections()
		var plugins []services.WorkbenchPlugin
		if err == nil {
			plugins, err = remote.WorkbenchPlugins()
		}
		var tasks []sourceObject
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
		var detail common.WorkbenchSnapshot
		var detailErr error
		if syncID != "" {
			detail, detailErr = remote.WorkbenchSyncDetails(syncID)
		}
		mygo.RunOnMain(func() {
			if m.win == nil || m.win.IsDestroyed() {
				return
			}
			m.liveBusy = false
			if generation != m.liveGeneration {
				m.refreshLive()
				return
			}
			detailChanged := false
			if syncID != "" && syncID == m.syncLogID && detailErr == nil {
				detailChanged = !reflect.DeepEqual(detail, m.syncDetail)
				m.syncDetail = detail
			}
			hadError := m.liveError
			m.liveError = err != nil
			if err == nil && (!reflect.DeepEqual(connections, m.liveConnections) || !reflect.DeepEqual(plugins, m.livePlugins) || !reflect.DeepEqual(tasks, m.objects)) {
				m.liveConnections = connections
				m.livePlugins = plugins
				m.applyLive(connections, tasks)
				m.win.Invalidate()
			} else if hadError != m.liveError || detailChanged {
				m.win.Invalidate()
			}
		})
	}()
}

func (m *mockup) applyLive(snapshots []common.WorkbenchSnapshot, tasks []sourceObject) {
	var connections []connection
	kinds := services.WorkbenchConnectors()
	for _, snapshot := range snapshots {
		for _, kind := range kinds {
			if kind.Kind != snapshot.Kind {
				continue
			}
			draft := m.connectorDraft(kind)
			draft.ID, draft.Name, draft.Version = snapshot.ID, snapshot.Name, snapshot.Version
			draft.Paused = snapshot.Paused
			draft.PluginDisabled = snapshot.PluginDisabled
			draft.WriteEnabled, draft.BridgeConfigured = snapshot.WriteEnabled, snapshot.BridgeConfigured
			if existing := m.connectionByID(draft.ID); existing != nil && existing.Name != draft.Name && m.sourceFilter == existing.Name {
				m.sourceFilter = draft.Name
			}
			draft.Scope, draft.VaultPath, draft.Repository = snapshot.Scope, snapshot.Vault, snapshot.Repository
			draft.ReadMethod = snapshot.ReadMethod
			draft.Status, draft.Capabilities, draft.Identity = "等待首次同步", "只读", "本地"
			if snapshot.Kind == common.WorkbenchObsidianTasks && snapshot.Capabilities.Create {
				draft.Writable = true
				draft.Capabilities = "读写"
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
			if snapshot.LastSuccess != nil {
				draft.LastConfirmedAt = snapshot.LastSuccess.Local().Format(time.DateTime)
			}
			delete(m.pendingSync, draft.ID)
			if len(snapshot.Attempts) > 0 {
				switch snapshot.Attempts[0].State {
				case "running":
					draft.ActiveAttempt = snapshot.Attempts[0].ID
					m.pendingSync[draft.ID] = true
				case "success":
					draft.Status = "同步正常"
				case "failed":
					draft.Status = "连接断开"
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
			m.syncLogs[draft.ID] = events
			connections = append(connections, draft)
		}
	}
	m.connectionsData = connections
	m.replaceLiveTasks(tasks)
}

func (m *mockup) replaceLiveTasks(tasks []sourceObject) {
	selectedID := m.taskSelection
	if m.inspector && m.selected >= 0 && m.selected < len(m.objects) {
		selectedID = m.objects[m.selected].ID
	}
	m.objects = tasks
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

func (m *mockup) saveLiveConnection(config connection, bridge common.WorkbenchBridgeInput) string {
	c := common.WorkbenchConnection{ID: config.ID, Version: config.Version, Kind: config.SourceKind, Name: config.Name, Vault: config.VaultPath, ReadMethod: config.ReadMethod, Scope: config.Scope, Repository: config.Repository}
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
