package workbench

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestEmptyFrameworkNavigationAndConnectorScaffolds(t *testing.T) {
	m := newFramework()
	m.useAdaptiveLayout()
	preview := ui.NewTester(m.view, 680, 580)

	for index, page := range pages {
		click(t, preview, page)
		if m.page != index {
			t.Fatalf("selected page %d, want %s (%d)", m.page, page, index)
		}
	}
	if !preview.HasText("暂无用量数据") || preview.HasText("最近 7 天 · 演示数据") {
		t.Fatal("empty workbench should show an honest usage state")
	}
	if !preview.HasText("有模型请求或同步记录后，会在这里显示用量。") {
		t.Fatal("usage should explain when data becomes available")
	}
	click(t, preview, "定时任务")
	if !preview.HasText("在 Apilot 默认模式中管理定时任务和运行历史。") {
		t.Fatal("scheduled tasks should explain where to find existing automation")
	}
	click(t, preview, "Now")
	if !preview.HasText("暂无简报") || !preview.HasText("来源同步后，Now 会汇集值得关注的变化。") {
		t.Fatal("Now should explain what powers the brief")
	}
	click(t, preview, "查看连接器")
	if m.page != connectionsPage {
		t.Fatal("the Now action should open the connector list")
	}
	click(t, preview, "任务")
	if !preview.HasText("暂无任务") || !preview.HasText("任务会在来源同步后出现在这里。") {
		t.Fatal("the task list should explain how it fills")
	}
	click(t, preview, "查看连接器")
	if m.page != connectionsPage {
		t.Fatal("the empty task action should open the connector list")
	}
	click(t, preview, "任务", "问助理")
	if !m.assistantOpen || !preview.HasText("助理尚未接入") || !preview.HasText("接入推理服务后，可在这里提问。") {
		t.Fatal("empty workbench should explain what the assistant needs")
	}
	preview.Key(0, ui.KeyEscape)
	if m.assistantOpen || m.page != tasksPage || len(m.objects) != 0 {
		t.Fatal("closing the assistant should return to the empty task list")
	}

	click(t, preview, "连接")
	if !preview.HasText("通过齿轮查看配置。同步尚未接入，配置只在本次运行中保留。") {
		t.Fatal("connection scaffolds should explain what can be configured and what lasts")
	}
	for _, scaffold := range []struct {
		kind, name, field string
	}{
		{githubIssues, "GitHub Issues", "仓库"},
		{obsidianTasks, "Obsidian Tasks", "Vault 路径"},
	} {
		conn := m.connectionByID(scaffold.kind)
		if conn == nil || conn.Status != "未接入" || conn.Writable {
			t.Fatalf("%s should start disconnected and read-only, got %+v", scaffold.name, conn)
		}
		click(t, preview, "连接配置 · "+scaffold.name)
		if !preview.HasText(scaffold.field) {
			t.Fatalf("%s is missing the configuration field %q", scaffold.name, scaffold.field)
		}
		click(t, preview, "取消")
		m.syncConnection(conn, func(_ time.Duration, _ func()) {
			t.Fatalf("%s scaffold started a simulated sync", scaffold.name)
		})
		if m.syncAttempts[conn.ID] != 0 || len(m.syncLogs[conn.ID]) != 0 {
			t.Fatalf("%s scaffold recorded a sync", scaffold.name)
		}
	}

	previousLanguage := englishUI.Swap(true)
	t.Cleanup(func() { englishUI.Store(previousLanguage) })
	for _, size := range [][2]int{{680, 580}, {1190, 730}} {
		preview.SetSize(size[0], size[1])
		click(t, preview, "Tasks")
		if !preview.HasText("No tasks yet") {
			t.Fatalf("missing English task guidance at %v", size)
		}
		click(t, preview, "View Connectors")
		if m.page != connectionsPage {
			t.Fatalf("English guidance did not open the connector list at %v", size)
		}
	}
}
