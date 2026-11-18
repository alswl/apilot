package workbench

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

//go:embed fixtures/usage.json
var usageJSON []byte

type usageFixture struct {
	Models []struct {
		Name                    string
		Requests, Input, Output int
		EstimatedUSD            float64 `json:"estimated_usd"`
	}
	Sessions []struct {
		Name             string
		Requests, Tokens int
	}
	Syncs []struct {
		ConnectionID       string `json:"connection_id"`
		Attempts, Failures int
	}
}

var demoUsage = func() usageFixture {
	var data usageFixture
	if err := json.Unmarshal(usageJSON, &data); err != nil {
		panic(err)
	}
	return data
}()

type usageMetric struct {
	title, value, icon string
}

func (m *mockup) usage(c *ui.Context) {
	ui.Tabs(c.Key("tabs"), &m.usageTab, tr("模型"), tr("会话"), tr("同步")).FillWidth()
	if m.framework {
		components.Muted(c.Key("period"), tr("暂无用量数据"))
		components.Muted(c.Key("guidance"), tr("有模型请求或同步记录后，会在这里显示用量。"))
		return
	}
	components.Muted(c.Key("period"), tr("最近 7 天 · 演示数据"))
	ui.Divider(c)
	switch m.usageTab {
	case 0:
		m.modelUsage(c.Key("models"))
	case 1:
		m.sessionUsage(c.Key("sessions"))
	case 2:
		m.syncUsage(c.Key("syncs"))
	}
}

func usageMetrics(c *ui.Context, metrics ...usageMetric) {
	ui.Row(c).FillWidth().Wrap().Gap(16).Children(func() {
		for _, metric := range metrics {
			ui.Column(c.Key(metric.title)).MinWidth(124).Grow(1).Gap(5).Padding(8, 0).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					components.Symbol(c, metric.icon, c.Theme().TextMuted)
					components.Muted(c, tr(metric.title))
				})
				ui.Text(c, metric.value).FontSize(22).FontWeight(600)
			})
		}
	})
}

func (m *mockup) modelUsage(c *ui.Context) {
	var requests, tokens int
	var cost float64
	var bars, requestBars, costBars []components.ChartBar
	for _, model := range demoUsage.Models {
		requests += model.Requests
		tokens += model.Input + model.Output
		cost += model.EstimatedUSD
		bars = append(bars, countBar(tr(model.Name)+" · "+tr("输入"), model.Input), countBar(tr(model.Name)+" · "+tr("输出"), model.Output))
		requestBars = append(requestBars, countBar(tr(model.Name), model.Requests))
		costBars = append(costBars, components.ChartBar{Label: tr(model.Name), Value: model.EstimatedUSD, Caption: fmt.Sprintf("$%.2f", model.EstimatedUSD)})
	}
	usageMetrics(c, usageMetric{"请求", fmt.Sprint(requests), "arrow.left.arrow.right"}, usageMetric{"Token", countCaption(tokens), "text.alignleft"}, usageMetric{"估算费用", fmt.Sprintf("$%.2f", cost), "dollarsign.circle"})
	components.BarChart(c.Key("tokens"), tr("输入与输出 Token"), bars)
	components.BarChart(c.Key("requests"), tr("按模型请求次数"), requestBars)
	components.BarChart(c.Key("cost"), tr("按模型估算费用"), costBars)
}

func (m *mockup) sessionUsage(c *ui.Context) {
	var bars, requests []components.ChartBar
	for _, session := range demoUsage.Sessions {
		bars = append(bars, countBar(tr(session.Name), session.Tokens))
		requests = append(requests, countBar(tr(session.Name), session.Requests))
	}
	usageMetrics(c, usageMetric{"会话与助理入口", fmt.Sprint(len(bars)), "bubble.left.and.bubble.right"})
	components.BarChart(c.Key("tokens"), tr("按会话或入口的 Token"), bars)
	components.BarChart(c.Key("requests"), tr("按会话或入口的请求"), requests)
}

func (m *mockup) syncUsage(c *ui.Context) {
	byID := make(map[string][2]int)
	for _, sync := range demoUsage.Syncs {
		byID[sync.ConnectionID] = [2]int{sync.Attempts, sync.Failures}
	}
	var attempts, failures int
	var runs, errors []components.ChartBar
	for _, conn := range m.connectionsData {
		counts := byID[conn.ID]
		counts[0] += m.syncAttempts[conn.ID]
		attempts += counts[0]
		failures += counts[1]
		runs = append(runs, countBar(tr(conn.Name), counts[0]))
		errors = append(errors, countBar(tr(conn.Name), counts[1]))
	}
	usageMetrics(c, usageMetric{"同步次数", fmt.Sprint(attempts), "arrow.triangle.2.circlepath"}, usageMetric{"失败次数", fmt.Sprint(failures), "exclamationmark.circle"})
	components.BarChart(c.Key("attempts"), tr("按连接同步次数"), runs)
	components.BarChart(c.Key("failures"), tr("按连接失败次数"), errors)
}

func countBar(label string, n int) components.ChartBar {
	return components.ChartBar{Label: label, Value: float64(n), Caption: fmt.Sprint(n)}
}

func countCaption(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}
