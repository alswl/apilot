package workbench

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorRunPanel struct {
	query         common.WorkbenchSyncQuery
	items         []common.WorkbenchSyncRun
	detail        *common.WorkbenchSyncRun
	problem       string
	loading, busy bool
}

func (m *mockup) openConnectorRuns(query common.WorkbenchSyncQuery, id string) {
	p := &connectorRunPanel{query: query}
	m.connectorRuns = p
	if id != "" {
		m.inspectConnectorRun(p, id)
	} else {
		m.loadConnectorRuns(p)
	}
}

func (m *mockup) loadConnectorRuns(p *connectorRunPanel) {
	if p.loading || p.detail != nil {
		return
	}
	p.loading = true
	query := p.query
	connectorUIRequest(m, "workbench.sync.list", query, func(value connectorItems[common.WorkbenchSyncRun], err error) {
		if m.connectorRuns != p {
			return
		}
		p.loading = false
		p.problem = ""
		if err != nil {
			p.problem = common.ProtocolError(err).Message
			return
		}
		p.items = value.Items
	})
}

func (m *mockup) inspectConnectorRun(p *connectorRunPanel, id string) {
	p.loading = true
	connectorUIRequest(m, "workbench.sync.inspect", map[string]string{"id": id}, func(value common.WorkbenchSyncRun, err error) {
		if m.connectorRuns != p {
			return
		}
		p.loading = false
		if err != nil {
			p.problem = common.ProtocolError(err).Message
			return
		}
		p.detail = &value
	})
}

func (m *mockup) connectorRunsDialog(c *ui.Context) {
	p := m.connectorRuns
	if p == nil {
		return
	}
	open := true
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(500).Gap(12).Children(func() {
			components.Title(c.Key("title"), tr("同步日志"))
			if p.detail == nil {
				options := []string{"全部结果", "success", "failed", "running", "access_denied", "auth_required", "cancelled"}
				selected := p.query.Status
				if selected == "" {
					selected = "全部结果"
				}
				labels := make([]string, len(options))
				for i, option := range options {
					labels[i] = connectorStatus(tr(option))
				}
				index := 0
				for i, option := range options {
					if option == selected {
						index = i
					}
				}
				label := labels[index]
				if ui.Select(c.Key("status"), &label, labels).Disabled(p.loading).Changed() {
					for i, candidate := range labels {
						if candidate == label {
							p.query.Status = options[i]
							if i == 0 {
								p.query.Status = ""
							}
							p.query.Offset = 0
							m.loadConnectorRuns(p)
							break
						}
					}
				}
			}
			ui.Scroll(c.Key("scroll")).FillWidth().MaxHeight(max(150, min(height-240, 430))).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					if p.detail != nil {
						m.connectorRunDetail(c.Key("detail"), p)
						return
					}
					if !p.loading && len(p.items) == 0 {
						components.Muted(c.Key("empty"), tr("暂无同步记录"))
					}
					for _, run := range p.items {
						label := m.connectionName(run.ScopeID) + " · " + connectorStatus(run.Mode) + " · " + connectorStatus(run.Status)
						if ui.Button(c.Key(run.ID), label).Clicked() {
							m.inspectConnectorRun(p, run.ID)
						}
						components.Muted(c.Key(run.ID+"-time"), run.StartedAt.Local().Format(time.DateTime))
					}
				})
			})
			if p.problem != "" {
				components.Muted(c.Key("problem"), p.problem)
			}
			if p.loading {
				components.StatusLabel(c.Key("loading"), components.Status{Text: tr("正在读取同步记录"), Loading: true, Color: c.Theme().TextMuted})
			}
			ui.Row(c.Key("footer")).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if p.detail != nil && len(p.items) > 0 {
					if ui.Button(c.Key("back"), tr("返回")).Clicked() {
						p.detail = nil
						p.problem = ""
					}
				} else if p.detail == nil {
					if ui.Button(c.Key("previous"), tr("上一页")).Disabled(p.loading || p.query.Offset == 0).Clicked() {
						p.query.Offset = max(0, p.query.Offset-50)
						m.loadConnectorRuns(p)
					}
					if ui.Button(c.Key("next"), tr("下一页")).Disabled(p.loading || len(p.items) < 50).Clicked() {
						p.query.Offset += 50
						m.loadConnectorRuns(p)
					}
				}
				if ui.Button(c.Key("close"), tr("关闭")).Clicked() {
					open = false
				}
			})
		})
	})
	if !open {
		m.connectorRuns = nil
	}
}

func (m *mockup) connectorRunDetail(c *ui.Context, p *connectorRunPanel) {
	r := p.detail
	components.Heading(c.Key("name"), m.connectionName(r.ScopeID)+" · "+connectorStatus(r.Status))
	components.Muted(c.Key("time"), connectorStatus(r.Mode)+" · "+r.StartedAt.Local().Format(time.DateTime))
	components.Muted(c.Key("trigger"), tr("触发方式")+" · "+r.Trigger)
	if r.FinishedAt != nil {
		components.Muted(c.Key("finished"), r.FinishedAt.Local().Format(time.DateTime))
	}
	if r.DurationMS != nil {
		components.Muted(c.Key("duration"), tr("耗时")+" · "+(time.Duration(*r.DurationMS)*time.Millisecond).String())
	}
	components.Body(c.Key("counts"), fmt.Sprintf(tr("%d 次请求 · %d 页 · %d 条拉取 · %d 条变更 · %d 次限流"), r.Requests, r.Pages, r.Fetched, r.Changed, r.RateLimits))
	if !r.UsageComplete {
		components.Muted(c.Key("partial"), tr("部分计量"))
	}
	if r.FailedStage != "" {
		components.Muted(c.Key("stage"), tr("失败阶段")+" · "+connectorStatus(r.FailedStage)+connectorProblem(r.ErrorKind))
	}
	if r.RetryAt != nil {
		components.Muted(c.Key("retry-at"), tr("可重试时间")+" · "+r.RetryAt.Local().Format(time.DateTime))
	}
	if r.Status == "running" {
		conn := m.connectionByID(r.ScopeID)
		if conn != nil && conn.ActiveAttempt != 0 && ui.Button(c.Key("cancel"), tr("取消同步")).Disabled(p.busy).Clicked() {
			p.busy = true
			connectorUIRequest[common.WorkbenchSnapshot](m, "workbench.sync.cancel", map[string]any{"id": r.ScopeID, "attempt_id": conn.ActiveAttempt}, func(_ common.WorkbenchSnapshot, err error) {
				p.busy = false
				if err != nil {
					p.problem = common.ProtocolError(err).Message
				}
				m.refreshLive()
				if m.connectorRuns == p {
					m.inspectConnectorRun(p, r.ID)
				}
			})
		}
		return
	}
	if r.ErrorKind == "auth_required" {
		if ui.Button(c.Key("relogin"), tr("重新登录原账号")).Clicked() {
			m.connectorRuns = nil
			m.openConnectorLogin(r.ConnectorID, r.ConnectionID)
		}
	} else if r.Status != "success" && ui.Button(c.Key("retry"), tr("立即同步")).Disabled(p.busy || (r.RetryAt != nil && time.Now().Before(*r.RetryAt))).Clicked() {
		p.busy = true
		connectorUIRequest[map[string]any](m, "workbench.sync.run", map[string]string{"id": r.ScopeID}, func(_ map[string]any, err error) {
			p.busy = false
			if err != nil {
				p.problem = common.ProtocolError(err).Message
				return
			}
			p.detail = nil
			p.query.ScopeID = r.ScopeID
			m.loadConnectorRuns(p)
			m.refreshLive()
		})
	}
}

type connectorUsageState struct {
	query                                 common.WorkbenchSyncQuery
	items                                 []common.WorkbenchSyncUsage
	loading, loaded                       bool
	problem                               string
	period, scope, connection, kind, mode string
}

func (m *mockup) loadConnectorUsage() {
	state := &m.connectorUsage
	if state.loading {
		return
	}
	state.loading = true
	query := state.query
	connectorUIRequest(m, "workbench.sync.usage", query, func(value connectorItems[common.WorkbenchSyncUsage], err error) {
		state.loading, state.loaded = false, true
		if state.query != query {
			state.loaded = false
			m.loadConnectorUsage()
			return
		}
		state.problem = ""
		if err != nil {
			state.problem = common.ProtocolError(err).Message
			return
		}
		state.items = value.Items
	})
}

func (m *mockup) managedSyncUsage(c *ui.Context) {
	state := &m.connectorUsage
	if !state.loaded && !state.loading {
		m.loadConnectorUsage()
	}
	connections := map[string]string{"全部连接": ""}
	scopes := map[string]string{"全部范围": ""}
	sources := map[string]string{"全部来源": ""}
	for _, p := range m.connectorParents {
		if !m.visibleConnectorKind(p.Connection.ConnectorID) {
			continue
		}
		display := p.Connection.EndpointRef
		for _, scope := range p.Scopes {
			if conn := m.connectionByID(scope.ScopeID); conn != nil && conn.Identity != "" {
				display = conn.Identity
				break
			}
		}
		if _, present := connections[display]; present {
			display += " · " + shortConnectorID(p.Connection.ID)
		}
		connections[display] = p.Connection.ID
		sources[connectorLabel(p.Connection.ConnectorID)] = p.Connection.ConnectorID
		for _, scope := range p.Scopes {
			label := scope.Label
			if _, present := scopes[label]; present {
				label += " · " + display
			}
			scopes[label] = scope.ScopeID
		}
	}

	ui.Row(c.Key("filters")).FillWidth().Wrap().Gap(8).Children(func() {
		changed := false
		if usageChoice(c.Key("connector"), &state.kind, sources, "全部来源", 120) {
			state.query.ConnectorID = sources[state.kind]
			changed = true
		}
		if usageChoice(c.Key("connection"), &state.connection, connections, "全部连接", 165) {
			state.query.ConnectionID = connections[state.connection]
			changed = true
		}
		if usageChoice(c.Key("scope"), &state.scope, scopes, "全部范围", 145) {
			state.query.ScopeID = scopes[state.scope]
			changed = true
		}
		if usageChoice(c.Key("period"), &state.period, map[string]string{"全部时间": "", "最近一天": "24", "最近七天": "168"}, "全部时间", 115) {
			state.query.From = ""
			switch state.period {
			case "最近一天":
				state.query.From = time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
			case "最近七天":
				state.query.From = time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
			}
			changed = true
		}
		modes := map[string]string{"全部模式": "", "全量同步": "full", "增量同步": "delta"}
		if usageChoice(c.Key("mode"), &state.mode, modes, "全部模式", 115) {
			state.query.Mode = modes[state.mode]
			changed = true
		}
		if changed {
			state.query.Offset = 0
			state.loaded = false
			if !state.loading {
				m.loadConnectorUsage()
			}
		}
	})
	if state.problem != "" {
		components.Muted(c.Key("problem"), state.problem)
	}
	if state.loading {
		components.Muted(c.Key("loading"), tr("正在读取同步记录"))
	}
	if state.loaded && len(state.items) == 0 {
		components.Muted(c.Key("empty"), tr("暂无同步记录"))
	}
	for _, item := range state.items {
		if !m.visibleConnectorKind(item.ConnectorID) {
			continue
		}
		key := item.ScopeID + "-" + item.Mode
		ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
			ui.Row(c.Key("heading")).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
				components.Heading(c.Key("scope"), m.connectionName(item.ScopeID)+" · "+connectorStatus(item.Mode))
				if ui.Button(c.Key("log"), tr("同步日志")).Clicked() {
					q := state.query
					q.ScopeID, q.Mode, q.Offset = item.ScopeID, item.Mode, 0
					m.openConnectorRuns(q, "")
				}
			})
			components.Body(c.Key("counts"), fmt.Sprintf(tr("%d 次同步 · %d 成功 · %d 失败 · %d 无变化"), item.Runs, item.Success, item.Failed, item.NoChanges))
			components.Muted(c.Key("metrics"), fmt.Sprintf(tr("%d 次请求 · %d 条拉取 · %d 次限流 · %s"), item.Requests, item.Fetched, item.RateLimits, (time.Duration(item.DurationMS)*time.Millisecond).String()))
			if item.PartialRuns > 0 {
				components.Muted(c.Key("partial"), tr("部分计量"))
			}
			ui.Divider(c.Key("separator"))
		})
	}
}

func shortConnectorID(id string) string { return id[:min(6, len(id))] }

func usageChoice(c *ui.Context, selection *string, choices map[string]string, fallback string, width float32) bool {
	keys := sortedConnectorKeys(choices)
	if _, ok := choices[*selection]; !ok {
		*selection = fallback
	}
	old := *selection
	localizedSelect(c, selection, keys, width)
	return old != *selection
}

func sortedConnectorKeys[T any](items map[string]T) []string {
	return slices.Sorted(maps.Keys(items))
}
