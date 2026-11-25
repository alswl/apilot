package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type connectorEdit struct {
	connection
	problem             string
	githubAuthorization services.GitHubAuthorizationStatus
	githubAuthorizing   bool
	githubSetupCancel   func()
	githubNeedsInstall  bool
	githubRepositories  []string
	githubListing       bool
	saving              bool
	duplicateID         string
	bridge              common.WorkbenchBridgeInput
	permissionVault     string
	permissionScope     string
}

type syncEvent struct {
	at, message string
}

func (m *mockup) connectionByID(id string) *connection {
	for i := range m.connectionsData {
		if m.connectionsData[i].ID == id {
			return &m.connectionsData[i]
		}
	}
	return nil
}

func (m *mockup) connections(c *ui.Context) {
	if m.connectionReturnToTasks {
		if components.IconButton(c.Key("return-tasks"), "chevron.left", tr("返回")+tr("任务"), false).Clicked() {
			m.returnToTaskList()
		}
	}
	if m.framework && m.live == nil {
		components.Muted(c.Key("guidance"), tr("通过齿轮查看配置。同步尚未接入，配置只在本次运行中保留。"))
	}
	if m.liveError {
		components.Muted(c.Key("live-error"), tr("无法读取工作台数据"))
	}
	ui.Row(c.Key("actions")).FillWidth().Justify(ui.End).Children(func() {
		ui.MenuButton(c.Key("add"), tr("添加连接"), func(menu *ui.Menu) {
			if m.connectorDesktop() {
				for _, id := range []string{managedGitHub, managedFixture} {
					if m.visibleConnectorKind(id) && m.connectorRelease(id) != nil && menu.Item(connectorLabel(id)).Chosen() {
						m.openConnectorLogin(id, "")
					}
				}
			}
			for _, connector := range services.WorkbenchConnectors() {
				if m.connectorDesktop() && connector.Kind != common.WorkbenchObsidianTasks {
					continue
				}
				if connector.UserConfigurable && menu.Item(connector.Name).Chosen() {
					m.addConnector(connector.Kind)
				}
			}
		})
	})
	if m.connectorDesktop() {
		m.managedConnections(c.Key("managed"))
	}
	if m.connectorProblem != "" {
		components.Muted(c.Key("connector-problem"), m.connectorProblem)
	}
	for i := range m.connectionsData {
		if m.connectorDesktop() && m.isManagedConnector(m.connectionsData[i].SourceKind) {
			continue
		}
		m.connectorSummary(c.Key(m.connectionsData[i].ID), &m.connectionsData[i])
		ui.Divider(c.Key("after-" + m.connectionsData[i].ID))
	}
}

func (m *mockup) connectionGuidance(c *ui.Context, hint string) {
	ui.Column(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
		components.Muted(c, hint)
		button := ui.Button(c.Key("connections"), tr("查看连接器"))
		if m.page == tasksPage {
			m.focusTaskConnectionEntry(func() { button.Focus() })
		}
		if button.Clicked() {
			if m.page == tasksPage {
				m.openTaskConnections()
			} else {
				m.navigate(connectionsPage)
			}
		}
	})
}

func (m *mockup) connectorSummary(c *ui.Context, conn *connection) {
	ui.Column(c).FillWidth().Padding(8).Gap(10).Children(func() {
		ui.Row(c.Key("header")).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			connectorLogo(c, conn.SourceKind, 22)
			ui.Column(c).Grow(1).MinWidth(160).Gap(4).Children(func() {
				components.Heading(c, tr(conn.Name))
				components.Muted(c, tr(conn.ConnectorID)+" · "+tr(conn.Scope))
			})
			log := components.IconButton(c.Key("log"), "list.bullet.rectangle", m.connectorLogTitle(conn)+" · "+tr(conn.Name), false)
			if m.syncLogRestoreFocus == conn.ID {
				log.Focus()
				m.syncLogRestoreFocus = ""
			}
			if log.Clicked() {
				m.syncLogID = conn.ID
				m.syncDetail = common.WorkbenchSnapshot{}
				m.syncDetailProblem = ""
				m.refreshLive()
			}
			if conn.SourceKind == common.WorkbenchLocal {
				return
			}
			if components.IconButton(c.Key("settings"), "gearshape", tr("连接配置")+" · "+tr(conn.Name), m.pendingSync[conn.ID]).Clicked() {
				m.editConnector(*conn)
			}
			label, icon := "立即同步", "arrow.clockwise"
			if conn.Status == "连接断开" {
				label, icon = "重新连接", "bolt.horizontal"
			}
			if components.IconButton(c.Key("sync"), icon, tr(label)+" · "+tr(conn.Name), (m.framework && m.live == nil) || conn.Paused || conn.PluginDisabled || m.pendingSync[conn.ID]).Clicked() {
				m.syncConnection(conn, m.after)
			}
		})
		ui.Row(c.Key("metadata")).FillWidth().Wrap().Gap(16).Children(func() {
			connectionField(c.Key("identity"), "person.crop.circle", tr("身份"), conn.Identity)
			connectionField(c.Key("capabilities"), "arrow.triangle.2.circlepath", tr("能力"), conn.Capabilities)
		})
		m.syncProgressView(c.Key("progress"), conn)
	})
}

func (m *mockup) syncProgressView(c *ui.Context, conn *connection) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		if progress := m.syncPhase[conn.ID]; m.pendingSync[conn.ID] {
			phase := "读取来源"
			if progress == 2 {
				phase = "应用增量"
			}
			label := tr(phase)
			liveProgress := conn.ReadProgress
			known := m.live != nil && liveProgress != nil && liveProgress.Total > 0
			if known {
				label = fmt.Sprintf(tr("已读取 %d / %d 篇笔记"), liveProgress.Read, liveProgress.Total)
			}
			components.StatusLabel(c.Key("phase"), components.Status{Text: label, Color: c.Theme().Accent, Loading: !known})
			if known {
				ui.Progress(c.Key("bar"), float64(liveProgress.Read)/float64(liveProgress.Total)).FillWidth().Label(tr("同步进度"))
			} else if m.live == nil {
				ui.Progress(c.Key("bar"), float64(progress)/3).FillWidth().Label(tr("同步进度"))
			}
		} else {
			icon, color := "checkmark.circle", c.Theme().Success
			if conn.Status != "同步正常" && conn.Status != "可用" {
				icon, color = "exclamationmark.circle", c.Theme().Warning
			}
			components.StatusLabel(c.Key("status"), components.Status{Text: tr(conn.Status), Icon: icon, Color: color})
		}
		if conn.RateLimitUntil != nil && conn.RateLimitUntil.After(time.Now()) {
			components.Muted(c.Key("rate-limit"), tr("GitHub 限流，等待至 ")+conn.RateLimitUntil.Local().Format("15:04:05"))
		}
		if conn.SourceKind == common.WorkbenchLocal {
			if conn.LastConfirmedAt != "—" {
				components.Muted(c, tr("上次导入 ")+conn.LastConfirmedAt)
			}
			return
		}
		components.Muted(c, tr("上次确认 ")+tr(conn.LastConfirmedAt))
	})
}

func (m *mockup) appendSyncEvent(id, message string) {
	events := append(m.syncLogs[id], syncEvent{time.Now().Format("15:04:05"), message})
	m.syncLogs[id] = events[max(0, len(events)-20):]
}

func (m *mockup) syncConnection(conn *connection, after func(time.Duration, func())) {
	id := conn.ID
	if m.live != nil {
		if err := m.live.SyncWorkbench(id); err == nil {
			m.refreshLive()
		} else {
			m.liveError = true
		}
		return
	}
	if m.framework || m.pendingSync[id] {
		return
	}
	m.pendingSync[id] = true
	m.syncPhase[id] = 1
	m.syncAttempts[id]++
	m.appendSyncEvent(id, "开始读取来源")
	after(300*time.Millisecond, func() {
		if m.pendingSync[id] {
			m.syncPhase[id] = 2
			m.appendSyncEvent(id, "正在应用增量")
		}
	})
	after(800*time.Millisecond, func() {
		if actual := m.connectionByID(id); actual != nil {
			actual.Status, actual.LastConfirmedAt = "同步正常", "刚刚"
			m.appendSyncEvent(id, "同步完成")
		}
		delete(m.pendingSync, id)
		delete(m.syncPhase, id)
	})
}

func (m *mockup) connectorLogTitle(conn *connection) string {
	if conn.SourceKind == common.WorkbenchLocal {
		return tr("导入记录")
	}
	return tr("同步日志")
}

func (m *mockup) syncLogDialog(c *ui.Context) {
	id := m.syncLogID
	open := id != ""
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		conn := m.connectionByID(m.syncLogID)
		if conn == nil {
			open = false
			return
		}
		ui.Column(c).Width(490).Gap(12).Children(func() {
			components.Title(c, m.connectorLogTitle(conn))
			components.Muted(c, tr(conn.Name)+" · "+tr(conn.ConnectorID))
			m.syncProgressView(c.Key("status"), conn)
			if m.live != nil && conn.SourceKind != common.WorkbenchLocal {
				label := "暂停同步"
				if conn.Paused {
					label = "恢复同步"
				}
				ui.Row(c.Key("sync-actions")).Gap(8).Children(func() {
					if ui.Button(c.Key("pause"), tr(label)).Clicked() {
						if err := m.live.PauseWorkbench(conn.ID, conn.Version, !conn.Paused); err != nil {
							m.liveError = true
						} else {
							m.refreshLive()
						}
					}
					if conn.ActiveAttempt != 0 && ui.Button(c.Key("cancel-sync"), tr("取消同步")).Clicked() {
						if err := m.live.CancelWorkbenchSync(conn.ID, conn.ActiveAttempt); err != nil {
							m.liveError = true
						} else {
							m.refreshLive()
						}
					}
				})
			}

			if m.live != nil && conn.ID != common.DefaultWorkbenchLocalID {
				if m.removeConnectionControl(c.Key("remove"), conn, &m.syncDetailProblem) {
					m.syncLogID = ""
					open = false
					return
				}
			}

			ui.Divider(c)
			events := m.syncLogs[conn.ID]
			if len(events) == 0 {
				components.Muted(c, tr("本次会话暂无同步记录"))
			}
			ui.Scroll(c.Key("events")).FillWidth().MaxHeight(max(64, min(240, height-315))).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					if m.live != nil {
						m.liveSyncResults(c.Key("write-results"), conn)
					}
					for i := len(events) - 1; i >= 0; i-- {
						components.ActivityRow(c.Key(i), events[i].at, tr(events[i].message))
					}
				})
			})
			if ui.Button(c.Key("close"), tr("关闭")).Clicked() {
				open = false
			}
		})
	})
	if !open && id != "" {
		if m.syncLogID == id && m.connectionByID(id) != nil {
			m.syncLogRestoreFocus = id
			c.Invalidate()
		}
		m.syncLogID = ""
		m.removeConnectionID = ""
	}
}

func (m *mockup) liveSyncResults(c *ui.Context, conn *connection) {
	if m.syncDetailProblem != "" {
		components.Muted(c.Key("error"), m.syncDetailProblem)
	}
	if m.syncDetail.ID != conn.ID {
		return
	}
	for _, receipt := range m.syncDetail.PendingWrites {
		label := "等待来源确认"
		switch receipt.SyncState {
		case common.WorkbenchOperationNeedsReview:
			label = "来源尚未确认"
		case common.WorkbenchOperationConflict:
			label = "来源已变化，请刷新写入位置"
		case common.WorkbenchOperationNotExecuted:
			label = "操作未执行"
		}
		ui.Column(c.Key(receipt.RequestID)).FillWidth().Gap(4).Children(func() {
			components.ActivityRow(c.Key("receipt"), receipt.RequestID, tr(label))
			if receipt.SyncState == common.WorkbenchOperationNeedsReview {
				button := ui.Button(c.Key("check"), tr("核对操作")).Label(tr("核对操作") + " · " + receipt.RequestID).Disabled(m.checkingOperations[receipt.RequestID])
				if button.Clicked() {
					m.reconcileLiveOperation(receipt.RequestID)
				}
			}
		})
	}
}

func (m *mockup) reconcileLiveOperation(requestID string) {
	if m.live == nil || requestID == "" || m.checkingOperations[requestID] {
		return
	}
	if m.checkingOperations == nil {
		m.checkingOperations = make(map[string]bool)
	}
	m.checkingOperations[requestID] = true
	connectionID := m.syncLogID
	remote := m.live
	go func() {
		receipt, err := remote.ReconcileWorkbenchTaskOperation(requestID)
		mygo.RunOnMain(func() {
			if !m.checkingOperations[requestID] {
				return
			}
			delete(m.checkingOperations, requestID)
			if m.syncLogID != connectionID {
				return
			}
			if err != nil {
				m.syncDetailProblem = common.ProtocolError(err).Message
			} else if receipt.SyncState == common.WorkbenchOperationNeedsReview {
				m.syncDetailProblem = tr("暂时无法确认操作结果")
			} else {
				m.syncDetailProblem = ""
			}
			m.liveGeneration++
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
				m.refreshLive()
			}
		})
	}()
}

func (m *mockup) removeConnectionControl(c *ui.Context, conn *connection, problem *string) bool {
	if m.removeConnectionID != conn.ID {
		if ui.Button(c.Key("start"), tr("解除连接")).Clicked() {
			m.removeConnectionID = conn.ID
			*problem = ""
			c.Invalidate()
		}
		return false
	}
	if conn.SourceKind == obsidianTasks {
		components.Muted(c.Key("scope"), tr("解除连接会移除工作台中的来源任务，保留原笔记。"))
	} else {
		components.Muted(c.Key("scope"), tr("解除连接会移除此连接及其任务。"))
	}
	removed := false
	ui.Row(c.Key("confirm")).Gap(8).Children(func() {
		if ui.Button(c.Key("cancel"), tr("保留连接")).Clicked() {
			m.removeConnectionID = ""
			*problem = ""
			c.Invalidate()
		}
		if ui.Button(c.Key("remove"), tr("确认解除连接")).Clicked() {
			if err := m.live.RemoveWorkbench(conn.ID); err != nil {
				*problem = common.ProtocolError(err).Message
				c.Invalidate()
				return
			}
			m.removeConnectionID = ""
			m.liveGeneration++
			m.refreshLive()
			removed = true
		}
	})
	return removed
}

func (m *mockup) connectorDraft(connector services.WorkbenchConnector) connection {
	draft := connection{
		SourceKind: connector.Kind, ConnectorID: connector.Name,
		Status: "等待首次同步", LastConfirmedAt: "—", Writable: true,
		Capabilities: connector.DemoCapabilities, SyncMode: connector.DemoSyncMode,
		SearchAliases: connector.SearchAliases, SearchFields: connector.SearchFields,
	}
	if m.live != nil {
		draft.Name = connector.Name
		draft.Writable = false
		if draft.SourceKind == githubIssues {
			draft.Repository = "alswl/issues"
		}
	} else if m.framework {
		draft.Status, draft.Capabilities, draft.Writable = "未接入", "未接入", false
	}
	return draft
}

func (m *mockup) seedConnectorScaffolds() {
	for _, connector := range services.WorkbenchConnectors() {
		if !connector.UserConfigurable {
			continue
		}
		draft := m.connectorDraft(connector)
		draft.ID, draft.Name, draft.Scope, draft.Identity = connector.Kind, connector.Name, "—", "—"
		m.connectionsData = append(m.connectionsData, draft)
	}
}

func (m *mockup) addConnector(kind string) {
	for _, connector := range services.WorkbenchConnectors() {
		if connector.Kind == kind && connector.UserConfigurable {
			m.editConnector(m.connectorDraft(connector))
			return
		}
	}
}

func (m *mockup) editConnector(conn connection) {
	conn.CheckboxMapping = maps.Clone(conn.CheckboxMapping)
	if conn.CheckboxMapping == nil {
		conn.CheckboxMapping = make(map[string]string)
	}
	conn.StatusLabels = maps.Clone(conn.StatusLabels)
	if conn.StatusLabels == nil {
		conn.StatusLabels = make(map[string]string)
	}
	m.connectorEditor = &connectorEdit{connection: conn, permissionVault: conn.VaultPath, permissionScope: conn.Scope}
	if m.live != nil && conn.SourceKind == common.WorkbenchGitHubRepository && conn.ID == "" {
		m.loadGitHubRepositories(m.connectorEditor, false)
	}
}

func (m *mockup) connectorDialog(c *ui.Context) {
	open := m.connectorEditor != nil
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		edit := m.connectorEditor
		ui.Column(c).Width(500).Gap(12).Children(func() {
			ui.Row(c.Key("heading")).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				connectorLogo(c, edit.SourceKind, 24)
				ui.Column(c).Gap(4).Children(func() {
					title := "连接配置"
					if edit.ID == "" {
						title = "添加连接"
					}
					components.Title(c, tr(title))
					components.Muted(c, edit.ConnectorID)
				})
			})
			ui.Scroll(c.Key("fields")).FillWidth().MaxHeight(min(460, height-150)).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					components.Heading(c.Key("basic-heading"), tr("基本信息"))
					if edit.SourceKind == githubIssues || edit.SourceKind == common.WorkbenchGitHubRepository {
						if m.live == nil || edit.SourceKind != common.WorkbenchGitHubRepository || edit.ID != "" {
							components.TextField(c.Key("name"), tr("连接名称"), &edit.Name).AutoFocus()
						}
						m.githubConnectorFields(c.Key("github"), edit)
					} else {
						m.obsidianConnectorFields(c.Key("obsidian"), edit)
					}
					if m.live != nil {
						if edit.SourceKind != githubIssues && edit.SourceKind != common.WorkbenchGitHubRepository {
							m.connectorMappingFields(c.Key("mapping"), edit)
							m.obsidianBridgeFields(c.Key("bridge"), edit)
						}
						return
					}
					ui.Divider(c.Key("sync-divider"))
					components.Heading(c.Key("sync-heading"), tr("同步与权限"))
					modes := []string{"文件变化", "定时拉取"}
					if edit.SourceKind == githubIssues {
						modes = []string{"定时拉取", "Webhook"}
					}
					ui.Row(c.Key("sync-options")).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							choiceField(c.Key("sync-mode"), "同步方式", &edit.SyncMode, modes)
						})
						ui.Checkbox(c.Key("writable"), &edit.Writable, tr("允许回写")).Disabled(m.framework)
					})
					ui.Divider(c.Key("mapping-divider"))
					components.Heading(c.Key("mapping-title"), tr("状态映射"))
					m.connectorMappingFields(c.Key("mapping"), edit)
				})
			})
			confirmingRemoval := m.removeConnectionID != "" && m.removeConnectionID == edit.ID
			if m.live != nil && edit.ID != "" && edit.ID != common.DefaultWorkbenchLocalID {
				if m.removeConnectionControl(c.Key("remove"), &edit.connection, &edit.problem) {
					open = false
					return
				}
			}
			if edit.problem != "" {
				components.Muted(c, tr(edit.problem))
			}
			if edit.saving {
				components.StatusLabel(c.Key("saving"), components.Status{Text: tr("正在验证连接…"), Loading: true, Color: c.Theme().TextMuted})
			}
			if edit.duplicateID != "" {
				if ui.Button(c.Key("existing"), tr("查看已有连接")).Clicked() {
					m.syncLogID = edit.duplicateID
					m.syncDetail = common.WorkbenchSnapshot{}
					m.refreshLive()
					open = false
				}
			}
			if confirmingRemoval {
				return
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Disabled(edit.saving).Clicked() {
					open = false
				}
				if m.live != nil && edit.SourceKind == common.WorkbenchGitHubRepository {
					m.gitHubConnectionAction(c, edit)
					return
				}
				if ui.PrimaryButton(c.Key("save"), tr("保存配置")).Disabled(edit.saving).Clicked() {
					if m.live != nil && edit.SourceKind == obsidianTasks {
						edit.bridge.WriteEnabled = &edit.WriteEnabled
						edit.problem = m.saveLiveConnection(edit.connection, edit.bridge)
					} else {
						edit.problem = m.saveConnector(edit.connection)
					}
					open = edit.problem != ""
				}
			})
		})
	})
	if !open && m.connectorEditor != nil && m.connectorEditor.saving {
		return
	}
	if !open && m.connectorEditor != nil {
		if m.connectorEditor.githubSetupCancel != nil {
			m.connectorEditor.githubSetupCancel()
		}
		if id, remote := m.connectorEditor.githubAuthorization.ID, m.live; id != "" && remote != nil {
			go func() { _ = remote.CancelGitHubAuthorization(id) }()
		}
		if m.removeConnectionID == m.connectorEditor.ID {
			m.removeConnectionID = ""
		}
		m.connectorEditor = nil
	}
}

func (m *mockup) obsidianBridgeFields(c *ui.Context, edit *connectorEdit) {
	ui.Checkbox(c.Key("writable"), &edit.WriteEnabled, tr("允许回写"))
}

func (m *mockup) githubConnectorFields(c *ui.Context, edit *connectorEdit) {
	if m.live != nil && edit.SourceKind == common.WorkbenchGitHubRepository {
		if edit.ID != "" {
			components.DataText(c.Key("repository"), edit.Repository)
		} else if len(edit.githubRepositories) > 0 {
			label := edit.Repository
			if label == "" {
				label = tr("选择仓库")
			}
			ui.MenuButton(c.Key("repository"), label, func(menu *ui.Menu) {
				for _, name := range edit.githubRepositories {
					if menu.Item(name).Chosen() {
						edit.Repository = name
						edit.problem = ""
					}
				}
				if menu.Item(tr("授权其他仓库…")).Chosen() {
					m.startGitHubSetup(edit)
				}
			})
		}
		if edit.githubListing {
			components.Muted(c.Key("loading-repositories"), tr("正在读取 GitHub 仓库…"))
		}
		if edit.githubNeedsInstall {
			components.Muted(c.Key("install-hint"), tr("选择 Only select repositories，只授权需要接入的仓库。"))
		}
		if edit.githubAuthorization.State == "authorized" {
			components.Muted(c.Key("authorized"), tr("GitHub 已授权"))
		} else if edit.githubAuthorization.State == "waiting" {
			components.Muted(c.Key("code"), tr("在 GitHub 输入验证码")+": "+edit.githubAuthorization.UserCode)
		}
		ui.Checkbox(c.Key("write"), &edit.WriteEnabled, tr("允许回写"))
		return
	}
	if m.live != nil {
		components.Muted(c, tr("模拟数据")+" · alswl/issues")
		return
	}
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c.Key("account")).Grow(1).MinWidth(0).Children(func() {
			components.TextField(c, tr("GitHub 账号"), &edit.Identity)
		})
		ui.Column(c.Key("repo")).Grow(1).MinWidth(0).Children(func() {
			components.TextField(c, tr("仓库"), &edit.Repository).
				Placeholder("owner/repository").Disabled(!m.framework && edit.ID != "")
		})
	})
}

func (m *mockup) gitHubConnectionAction(c *ui.Context, edit *connectorEdit) {
	label := "连接 GitHub"
	action := func() { m.saveLiveGitHubConnection(edit) }
	switch {
	case edit.githubAuthorization.State == "waiting":
		label = "复制验证码并打开 GitHub"
		action = func() { m.openGitHubVerification(edit) }
	case edit.githubNeedsInstall && edit.githubSetupCancel == nil:
		label = "选择 GitHub 授权仓库"
		action = func() { m.startGitHubSetup(edit) }
	case edit.githubNeedsInstall:
		label = "检查仓库授权"
		action = func() { m.continueGitHubSetup(edit) }
	case edit.ID != "":
		label = "保存配置"
	case len(edit.githubRepositories) == 0:
		label = "授权 GitHub"
		action = func() { m.loadGitHubRepositories(edit, true) }
	}
	unselected := len(edit.githubRepositories) > 0 && edit.Repository == "" && !edit.githubNeedsInstall
	if ui.PrimaryButton(c.Key("save"), tr(label)).Disabled(edit.saving || edit.githubAuthorizing || edit.githubListing || unselected).Clicked() {
		action()
	}
}

func (m *mockup) obsidianConnectorFields(c *ui.Context, edit *connectorEdit) {
	ui.Row(c.Key("identity")).FillWidth().Gap(12).Children(func() {
		ui.Column(c.Key("name")).Grow(1).MinWidth(0).Children(func() {
			components.TextField(c, tr("连接名称"), &edit.Name).AutoFocus()
		})
		ui.Column(c.Key("scope")).Grow(1).MinWidth(0).Children(func() {
			components.TextField(c, tr("同步范围"), &edit.Scope)
		})
	})
	ui.Row(c.Key("vault")).FillWidth().Gap(8).AlignItems(ui.End).Children(func() {
		ui.Column(c.Key("path")).Grow(1).MinWidth(0).Children(func() {
			components.TextField(c, tr("Vault 路径"), &edit.VaultPath)
		})
		if m.live != nil && m.win != nil && ui.Button(c.Key("choose"), tr("选择文件夹…")).Clicked() {
			m.chooseObsidianVault(edit)
		}
	})
	if edit.ID != "" && (edit.VaultPath != edit.permissionVault || edit.Scope != edit.permissionScope) {
		edit.WriteEnabled = false
		edit.permissionVault, edit.permissionScope = edit.VaultPath, edit.Scope
	}
}

func (m *mockup) chooseObsidianVault(edit *connectorEdit) {
	win := m.win
	options := mygo.OpenDialogOptions{
		Parent: win, Directory: true, DefaultPath: strings.TrimSpace(edit.VaultPath),
		Title: tr("选择 Obsidian vault"),
	}
	go func() {
		paths, err := mygo.Dialog.Open(options)
		mygo.RunOnMain(func() {
			if m.win != win || win.IsDestroyed() || m.connectorEditor != edit {
				return
			}
			if err != nil {
				edit.problem = "无法打开文件夹选择器"
			} else if len(paths) > 0 {
				edit.VaultPath = paths[0]
				edit.problem = ""
			} else {
				return
			}
			win.Invalidate()
		})
	}()
}

func (m *mockup) connectorMappingFields(c *ui.Context, edit *connectorEdit) {
	for _, mapping := range edit.stateMappings() {
		ui.Row(c.Key(mapping.label)).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			if edit.SourceKind == githubIssues {
				components.DataText(c, mapping.native).Width(132)
				label := edit.StatusLabels[mapping.label]
				input := ui.TextInput(c, &label).Grow(1).Label(tr("状态显示名") + " · " + mapping.native).Placeholder(tr(mapping.label))
				if input.Changed() {
					edit.StatusLabels[mapping.label] = strings.TrimSpace(label)
				}
			} else {
				if m.live != nil && (mapping.label == "待处理" || mapping.label == "已完成") {
					return
				}
				components.DataText(c, tr(mapping.label)).Width(132)
				value := mapping.native
				if value == " " {
					value = ""
				}
				input := ui.TextInput(c, &value).Width(92).Label(tr("复选框字符") + " · " + tr(mapping.label)).Placeholder(tr("空格"))
				if input.Changed() {
					edit.CheckboxMapping[mapping.label] = value
				}
			}
		})
	}
}

func (m *mockup) saveConnector(config connection) string {
	if m.live != nil {
		return m.saveLiveConnection(config, common.WorkbenchBridgeInput{})
	}
	config.CheckboxMapping = maps.Clone(config.CheckboxMapping)
	if config.CheckboxMapping == nil {
		config.CheckboxMapping = make(map[string]string)
	}
	config.Name, config.Identity = strings.TrimSpace(config.Name), strings.TrimSpace(config.Identity)
	if config.Name == "" || slices.ContainsFunc(m.connectionsData, func(conn connection) bool {
		return config.ID != conn.ID && strings.EqualFold(config.Name, conn.Name)
	}) {
		return "请填写唯一的连接名称"
	}
	if config.SourceKind == githubIssues || config.SourceKind == common.WorkbenchGitHubRepository {
		owner, repo, ok := strings.Cut(strings.TrimSpace(config.Repository), "/")
		if !ok || owner == "" || repo == "" || strings.ContainsAny(owner+repo, "/ \t") {
			return "仓库格式应为 owner/repository"
		}
		if config.Identity == "" {
			return "请填写 GitHub 账号"
		}
		config.Scope = config.Repository
		config.IssueLabels = slices.Clone(config.IssueLabels)
	} else {
		if strings.TrimSpace(config.VaultPath) == "" {
			return "请填写 Vault 路径"
		}
		seen := map[string]bool{}
		for _, state := range config.stateMappings() {
			char := state.native
			if char == "" {
				char = " "
			}
			if utf8.RuneCountInString(char) != 1 || strings.ContainsAny(char, "[]\n\r\t") || seen[char] {
				return "状态映射必须使用互不重复的单个字符"
			}
			seen[char] = true
			config.CheckboxMapping[state.label] = char
		}
		if config.Identity == "" {
			config.Identity = "本人（个人 vault）"
		}
	}
	if m.framework {
		config.Status, config.Capabilities, config.Writable = "未接入", "未接入", false
	}
	if existing := m.connectionByID(config.ID); existing != nil {
		if m.sourceFilter == existing.Name && (m.sourceFilterID == "" || m.sourceFilterID == existing.ID) {
			m.sourceFilter = config.Name
		}
		for id, view := range m.taskViewDrafts {
			if view.Source == existing.Name {
				view.Source = config.Name
				m.taskViewDrafts[id] = view
			}
		}
		for i := range m.savedTaskViews {
			if m.savedTaskViews[i].State.Source == existing.Name {
				m.savedTaskViews[i].State.Source = config.Name
			}
		}
		*existing = config
	} else {
		config.ID = fmt.Sprintf("connection-%d", len(m.connectionsData)+1)
		m.connectionsData = append(m.connectionsData, config)
	}
	return ""
}
