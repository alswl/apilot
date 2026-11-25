package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const managedGitHub = "github-issues"
const managedFixture = "fixture"

type connectorItems[T any] struct {
	Items []T `json:"items"`
}

func connectorCall[T any](remote *services.Remote, method string, input any) (T, error) {
	var value T
	raw, err := remote.Call(method, input)
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value, err
}

func connectorUIRequest[T any](m *mockup, method string, input any, finish func(T, error)) {
	remote := m.live
	go func() {
		value, err := connectorCall[T](remote, method, input)
		mygo.RunOnMain(func() { finish(value, err); m.invalidateLiveWindow() })
	}()
}

func (m *mockup) connectorDesktop() bool { return m.hosted && m.live != nil }

func (m *mockup) visibleConnectorKind(kind string) bool {
	if !m.connectorDesktop() {
		return true
	}
	switch kind {
	case managedGitHub, common.WorkbenchLocal, common.WorkbenchGitHubRepository, common.WorkbenchObsidianTasks:
		return true
	case managedFixture:
		return m.fixtureConnector
	default:
		return m.isManagedConnector(kind) || slices.ContainsFunc(m.livePlugins, func(plugin services.WorkbenchPlugin) bool { return plugin.ID == kind && plugin.Imported })
	}
}

func (m *mockup) isManagedConnector(kind string) bool {
	if isManagedConnector(kind) || m.connectorRelease(kind) != nil {
		return true
	}
	for _, parent := range m.connectorParents {
		if parent.Connection.ConnectorID == kind {
			return true
		}
	}
	return false
}

func isManagedConnector(kind string) bool { return kind == managedGitHub || kind == managedFixture }

func allowLocalConnectorFixture(executable, manifest string) bool {
	if manifest == "" {
		return false
	}
	for dir := filepath.Dir(executable); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if strings.EqualFold(filepath.Ext(dir), ".app") {
			return false
		}
	}
	return true
}

func localConnectorFixture() bool {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	return err == nil && allowLocalConnectorFixture(exe, os.Getenv("APILOT_DEV_CONNECTOR_MANIFEST"))
}

func (m *mockup) connectorRelease(id string) *common.WorkbenchConnectorRelease {
	for _, plugin := range m.livePlugins {
		if plugin.Enabled && plugin.Release != nil && plugin.Release.ConnectorID == id {
			return plugin.Release
		}
	}
	return nil
}

type connectorLoginPanel struct {
	kind, parent          string
	auth                  common.WorkbenchConnectorAuthorization
	choices               []protocol.ConfigOption
	selected              map[string]bool
	connected             map[string]bool
	results               []common.WorkbenchConnectorScopeResult
	next, problem         string
	write, busy           bool
	saving                bool
	scopeType, search     string
	scopeTypes            []string
	scopeConfig           *protocol.ConfigSchema
	endpointSchemaVersion int
	writable              bool
	queryGeneration       uint64
	cancelQuery           context.CancelFunc
}

func (m *mockup) openConnectorLogin(kind, parent string) {
	panel := &connectorLoginPanel{kind: kind, parent: parent, selected: map[string]bool{}, connected: map[string]bool{}, endpointSchemaVersion: 1, writable: kind == managedGitHub || kind == managedFixture}
	for _, plugin := range m.livePlugins {
		if plugin.Release == nil || plugin.Release.ConnectorID != kind {
			continue
		}
		panel.scopeConfig = plugin.ScopeConfig
		panel.writable = plugin.Writable || panel.writable
		if plugin.EndpointSchemaVersion > 0 {
			panel.endpointSchemaVersion = plugin.EndpointSchemaVersion
		}
		if panel.scopeConfig != nil {
			panel.scopeTypes = panel.scopeConfig.Types
		}
		break
	}
	if len(panel.scopeTypes) != 0 {
		panel.scopeType = panel.scopeTypes[0]
		if slices.Contains(panel.scopeTypes, protocol.ScopeProject) {
			panel.scopeType = protocol.ScopeProject
		}
	}
	m.connectorLogin = panel
}

func (m *mockup) startConnectorLogin(p *connectorLoginPanel) {
	p.busy = true
	p.problem = ""
	connectorUIRequest(m, "workbench.connector.login", common.WorkbenchConnectorLoginInput{ConnectorID: p.kind, ConnectionID: p.parent},
		func(auth common.WorkbenchConnectorAuthorization, err error) {
			if err != nil {
				if m.connectorLogin == p {
					p.busy = false
					p.problem = common.ProtocolError(err).Message
				}
				return
			}
			if m.connectorLogin != p {
				go func() { _, _ = m.live.Call("workbench.connector.cancel-login", map[string]string{"id": auth.ID}) }()
				return
			}
			p.auth = auth
			p.busy = false
			m.pollConnectorLogin(p)
		})
}

func (m *mockup) pollConnectorLogin(p *connectorLoginPanel) {
	if m.connectorLogin != p || p.auth.Status != "waiting" {
		return
	}
	id := p.auth.ID
	m.after(400*time.Millisecond, func() {
		if m.connectorLogin != p || p.auth.ID != id {
			return
		}
		connectorUIRequest(m, "workbench.connector.auth-status", map[string]string{"id": id}, func(auth common.WorkbenchConnectorAuthorization, err error) {
			if m.connectorLogin != p || p.auth.ID != id {
				return
			}
			if err != nil {
				p.problem = common.ProtocolError(err).Message
				m.pollConnectorLogin(p)
				return
			}
			p.auth = auth
			p.problem = ""
			switch auth.Status {
			case "waiting":
				m.pollConnectorLogin(p)
			case "authorized":
				if p.parent != "" {
					m.connectorLogin = nil
					m.refreshLive()
					return
				}
				m.connectorRecentAuthorization = auth
				m.loadConnectorChoices(p, "")
			default:
				p.problem = tr("授权未完成") + " · " + connectorStatus(auth.Status)
			}
		})
	})
}

func (p *connectorLoginPanel) field() string {
	if p.scopeConfig != nil {
		if _, ok := p.scopeConfig.Fields[p.scopeType]; ok {
			return p.scopeType
		}
		for _, field := range slices.Sorted(maps.Keys(p.scopeConfig.Fields)) {
			meta := p.scopeConfig.Fields[field]
			if meta.DynamicOptions || meta.Control == "select" {
				return field
			}
		}
	}
	if p.kind == managedFixture {
		return "project"
	}
	return "repository_id"
}

func (p *connectorLoginPanel) setScopeType(kind string) {
	p.scopeType, p.search, p.next, p.problem = kind, "", "", ""
	p.choices = nil
	clear(p.selected)
	clear(p.connected)
	p.queryGeneration++
}

func (m *mockup) loadConnectorChoices(p *connectorLoginPanel, page string) {
	p.busy = true
	if p.cancelQuery != nil {
		p.cancelQuery()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelQuery = cancel
	p.queryGeneration++
	generation := p.queryGeneration
	field := p.field()
	input := map[string]string{"id": p.kind, "authorization_id": p.auth.ID, "field": field}
	if p.scopeType != "" {
		input["scope_type"] = p.scopeType
	}
	if p.search != "" {
		input["search"] = p.search
	}
	if page != "" {
		input["page"] = page
	}
	finish := func(value common.WorkbenchConnectorOptions, err error) {
		if m.connectorLogin != p || p.queryGeneration != generation {
			return
		}
		p.busy = false
		p.cancelQuery = nil
		if err != nil {
			problem := common.ProtocolError(err)
			p.problem = problem.Message
			if problem.Code == "NOT_FOUND" || problem.Code == "CONFLICT" {
				if m.connectorRecentAuthorization.ID == p.auth.ID {
					m.connectorRecentAuthorization = common.WorkbenchConnectorAuthorization{}
				}
				p.auth = common.WorkbenchConnectorAuthorization{}
				p.choices, p.next = nil, ""
				clear(p.selected)
				clear(p.connected)
			}
			return
		}
		p.next = value.NextPage
		for _, connected := range value.ConnectedValues {
			p.connected[connected] = true
			delete(p.selected, connected)
		}
		for _, item := range value.Items {
			if !slices.ContainsFunc(p.choices, func(old protocol.ConfigOption) bool { return old.Value == item.Value }) {
				p.choices = append(p.choices, item)
			}
		}
	}
	remote := m.live
	go func() {
		defer cancel()
		var result common.WorkbenchConnectorOptions
		raw, err := remote.CallWorkbench(ctx, "workbench.connector.options", input)
		if err == nil {
			err = json.Unmarshal(raw, &result)
		}
		mygo.RunOnMain(func() { finish(result, err); m.invalidateLiveWindow() })
	}()

}

func (m *mockup) saveConnectorChoices(p *connectorLoginPanel) {
	scopeVersion := 1
	if p.scopeConfig != nil {
		scopeVersion = p.scopeConfig.Version
	}
	input := common.WorkbenchConnectorSaveInput{AuthorizationID: p.auth.ID, EndpointSchemaVersion: p.endpointSchemaVersion, ScopeSchemaVersion: scopeVersion}
	field := p.field()
	for _, choice := range p.choices {
		if p.selected[choice.Value] {
			input.Scopes = append(input.Scopes, common.WorkbenchConnectorSelection{Config: map[string]any{field: choice.Value}, ScopeType: p.scopeType, WriteEnabled: p.write && p.writable})
		}
	}
	if len(input.Scopes) == 0 {
		return
	}
	p.busy, p.saving = true, true
	p.problem = ""
	connectorUIRequest(m, "workbench.connector.connection.save", input, func(value common.WorkbenchConnectorSaveResult, err error) {
		if m.connectorLogin != p {
			return
		}
		p.busy, p.saving = false, false
		if err != nil {
			p.problem = common.ProtocolError(err).Message
			return
		}
		p.results = value.Scopes
		if m.connectorRecentAuthorization.ID == p.auth.ID && value.ConnectionID != "" {
			m.connectorRecentAuthorization = common.WorkbenchConnectorAuthorization{}
		}
		if value.ConnectionID != "" {
			if m.expandedConnectors == nil {
				m.expandedConnectors = map[string]bool{}
			}
			m.expandedConnectors[value.ConnectionID] = true
		}
		m.refreshLive()
	})
}

func (m *mockup) connectorLoginDialog(c *ui.Context) {
	p := m.connectorLogin
	if p == nil {
		return
	}
	open := true
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(480).Gap(12).Children(func() {
			components.Title(c.Key("title"), tr("添加连接"))
			if p.parent != "" {
				components.Muted(c.Key("relogin"), tr("重新登录原账号"))
			}
			if p.auth.Status == "waiting" {
				components.StatusLabel(c.Key("waiting"), components.Status{Text: tr("等待浏览器授权"), Loading: true, Color: c.Theme().TextMuted})
				if p.auth.UserCode != "" {
					components.Body(c.Key("code"), p.auth.UserCode)
				}
				openURLButton(c.Key("browser"), tr("打开授权页面"), p.auth.VerificationURL)
			}
			if p.auth.Status == "authorized" {
				components.Muted(c.Key("identity"), p.auth.DisplayName)
				if len(p.scopeTypes) > 1 && len(p.results) == 0 {
					selected := slices.Index(p.scopeTypes, p.scopeType)
					labels := make([]string, len(p.scopeTypes))
					for i, kind := range p.scopeTypes {
						labels[i] = tr("空间")
						if kind == protocol.ScopeProject {
							labels[i] = tr("项目")
						}
					}
					if ui.Segmented(c.Key("scope-types"), &selected, labels...).Disabled(p.saving).Changed() && selected >= 0 && selected < len(p.scopeTypes) {
						p.setScopeType(p.scopeTypes[selected])
						m.loadConnectorChoices(p, "")
					}
				}
				if len(p.scopeTypes) != 0 && len(p.results) == 0 {
					search := ui.TextInput(c.Key("search-scopes"), &p.search).Label(tr("搜索范围")).Placeholder(tr("搜索范围")).FillWidth().Disabled(p.saving)
					if search.Changed() {
						p.choices, p.next = nil, ""
						clear(p.selected)
						clear(p.connected)
						m.loadConnectorChoices(p, "")
					}
					if p.busy && !p.saving {
						components.StatusLabel(c.Key("loading-scopes"), components.Status{Text: tr("正在查找范围"), Loading: true, Color: c.Theme().TextMuted})
					} else if len(p.choices) == 0 && p.problem == "" {
						components.Muted(c.Key("no-scopes"), tr("没有符合条件的范围"))
					}
				}
				ui.Scroll(c.Key("scopes")).FillWidth().MaxHeight(max(160, min(height-250, 310))).Children(func() {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						for _, choice := range p.choices {
							if p.connected[choice.Value] {
								components.Muted(c.Key(choice.Value), choice.Label+" · "+tr("已连接"))
								continue
							}
							selected := p.selected[choice.Value]
							if ui.Checkbox(c.Key(choice.Value), &selected, choice.Label).Disabled(p.busy || len(p.results) > 0).Changed() {
								if selected {
									p.selected[choice.Value] = true
								} else {
									delete(p.selected, choice.Value)
								}
							}
						}
						if p.next != "" && ui.Button(c.Key("next"), tr("加载更多")).Disabled(p.busy || len(p.results) > 0).Clicked() {
							m.loadConnectorChoices(p, p.next)
						}
					})
				})
				if p.writable {
					ui.Checkbox(c.Key("write"), &p.write, tr("允许回写")).Disabled(p.busy || len(p.results) > 0)
				}
				for i, result := range p.results {
					status, problem := result.Status, result.ErrorKind
					if status == "queued" && result.ScopeID != "" {
						for _, parent := range m.connectorParents {
							for _, scope := range parent.Scopes {
								if scope.ScopeID == result.ScopeID && scope.LastRun != nil {
									status, problem = scope.LastRun.Status, scope.LastRun.ErrorKind
								}
							}
						}
					}
					components.Muted(c.Key(i), result.Label+" · "+connectorStatus(status)+connectorProblem(problem))
				}
			}
			if p.problem != "" {
				components.Muted(c.Key("problem"), p.problem)
			}
			ui.Row(c.Key("buttons")).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				closeLabel := "取消"
				if len(p.results) > 0 {
					closeLabel = "关闭"
				}
				if ui.Button(c.Key("cancel"), tr(closeLabel)).Clicked() {
					open = false
				}
				if p.auth.Status != "waiting" && p.auth.Status != "authorized" {
					recent := m.connectorRecentAuthorization
					if p.parent == "" && recent.ConnectorID == p.kind && recent.Status == "authorized" && time.Now().Before(recent.ExpiresAt) {
						if ui.Button(c.Key("reuse-account"), tr("使用账号")+" "+recent.DisplayName).Disabled(p.busy).Clicked() {
							p.auth = recent
							p.problem = ""
							m.loadConnectorChoices(p, "")
						}
					}
					if ui.PrimaryButton(c.Key("login"), tr("登录")).Disabled(p.busy).Clicked() {
						m.startConnectorLogin(p)
					}
				}
				if p.auth.Status == "authorized" && len(p.results) == 0 && ui.PrimaryButton(c.Key("save"), tr("保存所选范围")).Disabled(p.busy || len(p.selected) == 0 || len(p.selected) > 20).Clicked() {
					m.saveConnectorChoices(p)
				}
			})
		})
	})
	if !open {
		if p.cancelQuery != nil {
			p.cancelQuery()
		}
		if p.auth.Status == "waiting" {
			id := p.auth.ID
			connectorUIRequest[common.WorkbenchConnectorAuthorization](m, "workbench.connector.cancel-login", map[string]string{"id": id}, func(common.WorkbenchConnectorAuthorization, error) {})
		}
		m.connectorLogin = nil
	}
}

func connectorStatus(value string) string {
	switch value {
	case "running", "queued":
		return tr("等待同步")
	case "success", "available":
		return tr("同步正常")
	case "unknown":
		return tr("等待首次同步")
	case "rate_limited":
		return tr("已限流")
	case "temporary":
		return tr("来源暂不可用")
	case "unknown_field":
		return tr("来源字段已失效")
	case "incompatible_type":
		return tr("来源字段类型不匹配")
	case "read_only":
		return tr("只读")
	case "ambiguous_write":
		return tr("多个字段使用同一回写目标")
	case "read":
		return tr("读取来源")
	case "resolve":
		return tr("核对范围成员")
	case "publish":
		return tr("发布任务")
	case "failed", "sync_not_started":
		return tr("同步失败")
	case "access_denied":
		return tr("权限不足")
	case "auth_required":
		return tr("需要重新登录")
	case "cancelled":
		return tr("已取消")
	case "expired":
		return tr("授权已过期")
	case "duplicate_selection", "already_connected":
		return tr("已连接")
	case "rejected":
		return tr("无法保存")
	case "full":
		return tr("全量同步")
	case "delta":
		return tr("增量同步")
	case "interrupted":
		return tr("已中断")
	case "superseded":
		return tr("已替换")
	}
	return value
}

func connectorProblem(value string) string {
	if value == "" {
		return ""
	}
	return " · " + connectorStatus(value)
}

func openURLButton(c *ui.Context, label, raw string) {
	if raw != "" && ui.Button(c, label).Clicked() {
		c.OpenURL(raw)
	}
}

func (m *mockup) managedConnections(c *ui.Context) {
	for _, parent := range m.connectorParents {
		p := parent.Connection
		disabled := m.connectorRelease(p.ConnectorID) == nil
		if !m.visibleConnectorKind(p.ConnectorID) {
			continue
		}
		ui.Column(c.Key(p.ID)).FillWidth().Gap(8).Children(func() {
			ui.Row(c.Key("account")).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
				connectorLogo(c.Key("mark"), p.ConnectorID, 22)
				title := p.EndpointRef
				for _, scope := range parent.Scopes {
					if conn := m.connectionByID(scope.ScopeID); conn != nil && conn.Identity != "" {
						title = conn.Identity
						break
					}
				}
				components.Heading(c.Key("title"), title)
				components.Muted(c.Key("count"), fmt.Sprintf(tr("%d 个同步范围"), len(parent.Scopes)))
				if disabled {
					components.Muted(c.Key("disabled"), tr("插件已停用"))
				} else if p.AuthState != "authorized" {
					components.Muted(c.Key("authorization"), connectorStatus(p.AuthState))
				}
				expanded := m.expandedConnectors[p.ID]
				icon := "chevron.right"
				if expanded {
					icon = "chevron.down"
				}
				if components.IconButton(c.Key("expand"), icon, tr("同步范围"), false).Clicked() {
					if m.expandedConnectors == nil {
						m.expandedConnectors = map[string]bool{}
					}
					m.expandedConnectors[p.ID] = !expanded
				}
				if ui.Button(c.Key("all"), tr("同步全部")).Disabled(disabled || p.AuthState != "authorized").Clicked() {
					connectorUIRequest[connectorItems[common.WorkbenchConnectorScopeResult]](m, "workbench.connector.connection.sync", map[string]string{"id": p.ID}, func(value connectorItems[common.WorkbenchConnectorScopeResult], err error) {
						m.connectorProblem = ""
						if err != nil {
							m.connectorProblem = common.ProtocolError(err).Message
						}
						for _, scope := range value.Items {
							if scope.ErrorKind != "" {
								m.connectorProblem = scope.Label + connectorProblem(scope.ErrorKind)
								break
							}
						}
						m.refreshLive()
					})
				}
				if ui.Button(c.Key("relogin"), tr("重新登录原账号")).Disabled(disabled).Clicked() {
					m.openConnectorLogin(p.ConnectorID, p.ID)
				}
				if ui.Button(c.Key("logs"), tr("同步日志")).Clicked() {
					m.openConnectorRuns(common.WorkbenchSyncQuery{ConnectionID: p.ID}, "")
				}
			})
			if m.expandedConnectors[p.ID] {
				for _, scope := range parent.Scopes {
					m.managedScope(c.Key(scope.ScopeID), scope, disabled)
				}
			}
			ui.Divider(c.Key("separator"))
		})
	}
}

func (m *mockup) managedScope(c *ui.Context, scope common.WorkbenchConnectorScopeOverview, disabled bool) {
	ui.Column(c).FillWidth().Padding(8).Gap(4).Children(func() {
		ui.Row(c.Key("row")).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
			components.Heading(c.Key("name"), scope.Label)
			switch scope.ScopeType {
			case protocol.ScopeProject:
				components.Muted(c.Key("scope-type"), tr("项目"))
			case protocol.ScopeSpace:
				components.Muted(c.Key("scope-type"), tr("空间"))
			}
			status := scope.AccessState
			if scope.LastRun != nil {
				status = scope.LastRun.Status
			}
			if scope.AccessState == "auth_required" || scope.AccessState == "access_denied" {
				status = scope.AccessState
			}
			components.Muted(c.Key("status"), connectorStatus(status))
			if ui.Button(c.Key("tasks"), tr("查看任务")).Clicked() {
				m.navigate(tasksPage)
				m.sourceFilter, m.sourceFilterID = scope.Label, scope.ScopeID
				m.taskPageIndex = 0
			}
			if conn := m.connectionByID(scope.ScopeID); conn != nil {
				write := conn.WriteEnabled
				if ui.Checkbox(c.Key("write"), &write, tr("允许回写")).Disabled(disabled).Changed() {
					m.toggleConnectorScopeWrite(conn, write)
				}
			}
			if ui.Button(c.Key("mapping"), tr("字段映射")).Disabled(disabled || scope.AccessState != "available").Clicked() {
				m.openConnectorMapping(scope.ScopeID, "")
			}
			retryBlocked := disabled || scope.AccessState == "auth_required" || m.pendingSync[scope.ScopeID]
			if scope.LastRun != nil && scope.LastRun.RetryAt != nil {
				retryBlocked = retryBlocked || time.Now().Before(*scope.LastRun.RetryAt)
			}
			if ui.Button(c.Key("sync"), tr("立即同步")).Disabled(retryBlocked).Clicked() {
				m.pendingSync[scope.ScopeID] = true
				connectorUIRequest[map[string]any](m, "workbench.sync.run", map[string]string{"id": scope.ScopeID}, func(_ map[string]any, err error) {
					m.connectorProblem = ""
					if err != nil {
						delete(m.pendingSync, scope.ScopeID)
						m.connectorProblem = common.ProtocolError(err).Message
					}
					m.refreshLive()
				})
			}
			if ui.Button(c.Key("logs"), tr("同步日志")).Clicked() {
				m.openConnectorRuns(common.WorkbenchSyncQuery{ScopeID: scope.ScopeID}, "")
			}
		})
		if scope.LastSuccess != nil {
			components.Muted(c.Key("last"), scope.LastSuccess.Local().Format(time.DateTime))
		}
	})
}

func connectorLabel(id string) string {
	switch id {
	case managedGitHub, common.WorkbenchGitHubRepository:
		return "GitHub Issues"
	case managedFixture:
		return tr("本地测试来源")
	case common.WorkbenchLocal:
		return "Local Tasks"
	case common.WorkbenchObsidianTasks:
		return "Obsidian Tasks"
	default:
		return id
	}
}
