package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorEdit struct {
	connection
	problem string
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
	ui.Row(c.Key("actions")).FillWidth().Justify(ui.End).Children(func() {
		ui.MenuButton(c.Key("add"), tr("添加连接"), func(menu *ui.Menu) {
			if menu.Item("GitHub Issues").Chosen() {
				m.addConnector(githubIssues)
			}
			if menu.Item("Obsidian Tasks").Chosen() {
				m.addConnector(obsidianTasks)
			}
		})
	})
	for i := range m.connectionsData {
		m.connectorSummary(c.Key(m.connectionsData[i].ID), &m.connectionsData[i])
		ui.Divider(c.Key("after-" + m.connectionsData[i].ID))
	}
}

func connectorIcon(kind string) string {
	if kind == githubIssues {
		return "point.3.connected.trianglepath.dotted"
	}
	return "doc.text"
}

func (m *mockup) connectorSummary(c *ui.Context, conn *connection) {
	ui.Column(c).FillWidth().Padding(8).Gap(10).Children(func() {
		ui.Row(c.Key("header")).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			components.Symbol(c, connectorIcon(conn.SourceKind), c.Theme().TextMuted)
			ui.Column(c).Grow(1).MinWidth(160).Gap(4).Children(func() {
				components.Heading(c, tr(conn.Name))
				components.Muted(c, tr(conn.ConnectorID)+" · "+tr(conn.Scope))
			})
			if components.IconButton(c.Key("log"), "list.bullet.rectangle", tr("同步日志")+" · "+tr(conn.Name), false).Clicked() {
				m.syncLogID = conn.ID
			}
			if components.IconButton(c.Key("settings"), "gearshape", tr("连接配置")+" · "+tr(conn.Name), m.pendingSync[conn.ID]).Clicked() {
				m.editConnector(*conn)
			}
			label, icon := "立即同步", "arrow.clockwise"
			if conn.Status == "连接断开" {
				label, icon = "重新连接", "bolt.horizontal"
			}
			if components.IconButton(c.Key("sync"), icon, tr(label)+" · "+tr(conn.Name), m.pendingSync[conn.ID]).Clicked() {
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
			components.StatusLabel(c.Key("phase"), components.Status{Text: tr(phase), Color: c.Theme().Accent, Loading: true})
			ui.Progress(c.Key("bar"), float64(progress)/3).FillWidth().Label(tr("同步进度"))
		} else {
			icon, color := "checkmark.circle", c.Theme().Success
			if conn.Status != "同步正常" {
				icon, color = "exclamationmark.circle", c.Theme().Warning
			}
			components.StatusLabel(c.Key("status"), components.Status{Text: tr(conn.Status), Icon: icon, Color: color})
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
	if m.pendingSync[id] {
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

func (m *mockup) syncLogDialog(c *ui.Context) {
	open := m.syncLogID != ""
	ui.Modal(c, &open, func() {
		conn := m.connectionByID(m.syncLogID)
		if conn == nil {
			open = false
			return
		}
		ui.Column(c).Width(490).Gap(12).Children(func() {
			components.Title(c, tr("同步日志"))
			components.Muted(c, tr(conn.Name)+" · "+tr(conn.ConnectorID))
			m.syncProgressView(c.Key("status"), conn)
			ui.Divider(c)
			events := m.syncLogs[conn.ID]
			if len(events) == 0 {
				components.Muted(c, tr("本次会话暂无同步记录"))
			}
			ui.Scroll(c.Key("events")).FillWidth().MaxHeight(240).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
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
	if !open {
		m.syncLogID = ""
	}
}

func (m *mockup) addConnector(kind string) {
	draft := connection{SourceKind: kind, Status: "等待首次同步", LastConfirmedAt: "—", Writable: true}
	if kind == githubIssues {
		draft.ConnectorID, draft.Capabilities, draft.SyncMode = "GitHub Issues", "Issues 写入 · Webhook", "定时拉取"
		draft.SearchAliases = []string{"github", "issues"}
		draft.SearchFields = []string{"repo", "assignee", "milestone", "state", "state_reason"}
	} else {
		draft.ConnectorID, draft.Capabilities, draft.SyncMode = "Obsidian Tasks", "文件变化 · 增量读取 · 回写", "文件变化"
		draft.SearchAliases = []string{"obsidian"}
		draft.SearchFields = []string{"path", "section", "start", "scheduled", "done", "checkbox"}
	}
	m.editConnector(draft)
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
	m.connectorEditor = &connectorEdit{connection: conn}
}

func (m *mockup) connectorDialog(c *ui.Context) {
	open := m.connectorEditor != nil
	ui.Modal(c, &open, func() {
		edit := m.connectorEditor
		ui.Column(c).Width(500).Gap(12).Children(func() {
			components.Title(c, tr(edit.ConnectorID)+" · "+tr("连接配置"))
			ui.Scroll(c.Key("fields")).FillWidth().MaxHeight(395).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					components.TextField(c.Key("name"), tr("连接名称"), &edit.Name).AutoFocus()
					if edit.SourceKind == githubIssues {
						m.githubConnectorFields(c.Key("github"), edit)
					} else {
						m.obsidianConnectorFields(c.Key("obsidian"), edit)
					}
					ui.Checkbox(c.Key("writable"), &edit.Writable, tr("允许回写"))
				})
			})
			if edit.problem != "" {
				components.Muted(c, tr(edit.problem))
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c.Key("save"), tr("保存配置")).Clicked() {
					edit.problem = m.saveConnector(edit.connection)
					open = edit.problem != ""
				}
			})
		})
	})
	if !open {
		m.connectorEditor = nil
	}
}

func (m *mockup) githubConnectorFields(c *ui.Context, edit *connectorEdit) {
	components.TextField(c.Key("account"), tr("GitHub 账号"), &edit.Identity)
	components.TextField(c.Key("repository"), tr("仓库"), &edit.Repository).
		Placeholder("owner/repository").Disabled(edit.ID != "")
	choiceField(c.Key("sync-mode"), "同步方式", &edit.SyncMode, []string{"定时拉取", "Webhook"})
	components.Heading(c, tr("状态映射"))
	for _, mapping := range edit.stateMappings() {
		label := edit.StatusLabels[mapping.label]
		input := components.TextField(c.Key(mapping.label), mapping.native, &label).
			Label(tr("状态显示名") + " · " + mapping.native).Placeholder(tr(mapping.label))
		if input.Changed() {
			edit.StatusLabels[mapping.label] = strings.TrimSpace(label)
		}
	}
}

func (m *mockup) obsidianConnectorFields(c *ui.Context, edit *connectorEdit) {
	components.TextField(c.Key("vault"), tr("Vault 路径"), &edit.VaultPath)
	components.TextField(c.Key("scope"), tr("同步范围"), &edit.Scope)
	choiceField(c.Key("sync-mode"), "同步方式", &edit.SyncMode, []string{"文件变化", "定时拉取"})
	components.Heading(c, tr("状态映射"))
	for _, mapping := range edit.stateMappings() {
		value := mapping.native
		if value == " " {
			value = ""
		}
		input := components.TextField(c.Key(mapping.label), tr(mapping.label), &value).
			Label(tr("复选框字符") + " · " + tr(mapping.label)).Placeholder(tr("空格"))
		if input.Changed() {
			edit.CheckboxMapping[mapping.label] = value
		}
	}
}

func (m *mockup) saveConnector(config connection) string {
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
	if config.SourceKind == githubIssues {
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
	if existing := m.connectionByID(config.ID); existing != nil {
		if m.sourceFilter == existing.Name {
			m.sourceFilter = config.Name
		}
		for id, view := range m.taskViewDrafts {
			if view.source == existing.Name {
				view.source = config.Name
				m.taskViewDrafts[id] = view
			}
		}
		for i := range m.savedTaskViews {
			if m.savedTaskViews[i].state.source == existing.Name {
				m.savedTaskViews[i].state.source = config.Name
			}
		}
		*existing = config
	} else {
		config.ID = fmt.Sprintf("connection-%d", len(m.connectionsData)+1)
		m.connectionsData = append(m.connectionsData, config)
	}
	return ""
}
