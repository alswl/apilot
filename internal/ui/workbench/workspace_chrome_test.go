package workbench

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSettingsLanguageAndTabs(t *testing.T) {
	previous := englishUI.Load()
	englishUI.Store(false)
	t.Cleanup(func() { englishUI.Store(previous) })

	m := newMockup()
	m.applyLocale = func(tag string) { englishUI.Store(tag == "en") }
	p := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { m.settings(c) })
	}, 580, 470)
	click(t, p, "界面语言", "English")
	if m.languageChoice != "en" || !p.HasText("General") {
		t.Fatal("choosing English should immediately translate the settings tabs")
	}
	click(t, p, "Assistant")
	if m.settingsTab != 1 || !p.HasText("Default model") {
		t.Fatal("assistant settings should have their own tab")
	}
	click(t, p, "General", "Display language", "简体中文")
	if m.languageChoice != "zh-Hans" || !p.HasText("常规") {
		t.Fatal("choosing Simplified Chinese should translate the current settings window")
	}
}

func TestNowSessionsKeepDraftsAndUseFlatSidebar(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "每日简报", "追问这份简报")
	p.Type("先核对退款")
	click(t, p, "新建会话")
	if m.nowSessionIndex != 2 || m.nowSession().draft != "" || len(m.nowSession().turns) != 0 {
		t.Fatal("a new session should start with its own empty draft and history")
	}
	p.Type("看看发布进度")
	p.Key(0, ui.KeyEnter)
	if len(m.nowSession().turns) != 1 || m.nowSession().title != "看看发布进度" {
		t.Fatal("the new session should open with the composer focused and take its title from the first message")
	}
	click(t, p, "每日简报")
	if m.nowSession().draft != "先核对退款" || len(m.nowSession().turns) != 0 {
		t.Fatal("switching back should restore the briefing draft and its own history")
	}
	click(t, p, "收起 Now 会话")
	if p.HasText("每日简报") && p.HasText("发布检查") {
		t.Fatal("collapsing Now should hide the session list")
	}
	click(t, p, "展开 Now 会话")
	if p.HasText("展开会话来源 · 每日简报") || !p.HasText("发布检查") {
		t.Fatal("the expanded sidebar should list sessions without nesting task links")
	}
}

func TestWorkspaceAssistantModalAndSyncStatus(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "验证老订单退款", "问助理")
	if !m.assistantOpen || m.assistantContext != 0 {
		t.Fatal("the toolbar assistant should open a dialog for the selected task")
	}
	p.Type("当前是什么状态？")
	p.Key(0, ui.KeyEnter)
	if thread := m.assistantThreadFor(0); len(thread.turns) != 1 || thread.turns[0].question != "当前是什么状态？" {
		t.Fatal("the dialog should focus its composer and record the reply in the task conversation")
	}
	click(t, p, "关闭助理")
	if m.assistantOpen || m.taskSelection != "refund" {
		t.Fatal("closing the dialog should leave the task selected")
	}
	click(t, p, "2 个来源已同步")
	if m.page != connectionsPage || !p.HasText("GitHub Issues") || !p.HasText("Obsidian Tasks") || !p.HasText("能力") {
		t.Fatal("the sync status should lead to both connectors with visible metadata")
	}
	var finish func()
	m.syncConnection(&m.connectionsData[0], func(_ time.Duration, complete func()) { finish = complete })
	p.Frame()
	if !p.HasText("正在同步 1 个来源") {
		t.Fatal("the footer should show a pending sync")
	}
	finish()
	p.Frame()
	if !p.HasText("2 个来源已同步") {
		t.Fatal("the footer should update after sync completion")
	}
	click(t, p, "用量")
	if m.page != usagePage || !p.HasText("输入与输出 Token") {
		t.Fatal("the usage destination should show the model charts")
	}
	click(t, p, "会话")
	if !p.HasText("按会话或入口的 Token") {
		t.Fatal("the sessions tab should show session-attributed metrics")
	}
	click(t, p, "同步")
	if !p.HasText("按连接失败次数") {
		t.Fatal("the sync tab should show connector charts")
	}
	click(t, p, "问助理")
	if !m.assistantOpen || m.assistantContext != -1 {
		t.Fatal("the assistant should open the current Now conversation when no task is selected")
	}
	p.Key(0, ui.KeyEscape)
	if m.assistantOpen || m.page != usagePage {
		t.Fatal("Escape should close the conversation dialog and preserve the current page")
	}
}
