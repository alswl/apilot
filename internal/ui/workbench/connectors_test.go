package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/server"
	"github.com/alswl/apilot/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestConnectorInstancesAndSyncLog(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	m.navigate(connectionsPage)
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "连接配置 · 工作 vault")
	if !p.HasText("Vault 路径") || p.HasText("GitHub 账号") {
		t.Fatal("the Obsidian panel should show vault settings")
	}
	click(t, p, "取消", "连接配置 · 支付发布")
	if !p.HasText("GitHub 账号") || !p.HasText("状态映射") {
		t.Fatal("the GitHub panel should show its account and status mapping")
	}
	click(t, p, "状态显示名 · open")
	p.Type("待评审")
	click(t, p, "保存配置")
	if m.connectionsData[1].StatusLabels["开放"] != "待评审" {
		t.Fatal("saving the GitHub panel should apply the instance's state display mapping")
	}

	m.addConnector(githubIssues)
	draft := m.connectorEditor.connection
	draft.Name, draft.Identity, draft.Repository = "发布备份", "demo-alt", "example/releases"
	if problem := m.saveConnector(draft); problem != "" {
		t.Fatal(problem)
	}
	m.connectorEditor = nil
	p.Frame()
	if len(m.connectionsData) != 3 || !p.HasText("发布备份") || !p.HasText("支付发布") {
		t.Fatal("the same connector type should allow multiple instances")
	}

	id := m.connectionsData[2].ID
	var phases []func()
	m.syncConnection(m.connectionByID(id), func(_ time.Duration, action func()) { phases = append(phases, action) })
	p.Frame()
	if !p.HasText("读取来源") {
		t.Fatal("expected the initial sync phase")
	}
	phases[0]()
	p.Frame()
	if !p.HasText("应用增量") {
		t.Fatal("expected the applying phase")
	}
	phases[1]()
	p.Frame()
	click(t, p, "同步日志 · 发布备份")
	log := m.syncLogs[id]
	if !p.HasText("同步完成") || len(log) != 3 || len(m.syncLogs["team"]) != 0 {
		t.Fatal("each instance should keep an independent sync history")
	}
}

func TestTaskKeyboardOpenAndStatusMenu(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "验证老订单退款", "收起任务详情")
	if !m.layout.detailHidden || m.taskSelection != "refund" {
		t.Fatal("hiding the detail should leave the task selected")
	}
	p.Key(0, ui.KeyO)
	if m.layout.detailHidden || !m.fullTask || m.objects[m.selected].ID != "refund" {
		t.Fatal("o should reopen the selected task detail")
	}
	if err := p.RightClick("进行中"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.Menu(), "进行中") || !slices.Contains(p.Menu(), "不计划处理") {
		t.Fatalf("the task row should provide connector statuses, got %v", p.Menu())
	}
	if err := p.ChooseMenuItem("不计划处理"); err != nil {
		t.Fatal(err)
	}
	if !m.pendingTaskEdits["refund"] {
		t.Fatal("switching through the context menu should queue a source write")
	}
}

func TestObsidianStatusMappingWritesConfiguredCharacter(t *testing.T) {
	m := newMockup()
	conn := m.connectionsData[0]
	conn.CheckboxMapping = map[string]string{"已完成": "D"}
	if problem := m.saveConnector(conn); problem != "" {
		t.Fatal(problem)
	}
	draft := sourceDraft(m.objects[0], obsidianTasks)
	draft.state = "已完成"
	if problem := m.submitSourceEdit(draft, func(_ time.Duration, complete func()) { complete() }); problem != "" {
		t.Fatal(problem)
	}
	if fields := m.objects[0].NativeFields; fields["checkbox"] != "D" || !taskClosed(m.objects[0]) {
		t.Fatalf("configured completion should write D and remain terminal, got %v", fields)
	}
}

func TestLocalKeyValueLabelsCanBeSavedAndSearched(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "验证老订单退款")
	p.Key(0, ui.KeyL)
	click(t, p, "标签键")
	p.Type("area")
	click(t, p, "标签值")
	p.Type("payments")
	click(t, p, "添加键值标签", "保存标签")
	if m.objects[0].LabelValues["area"] != "payments" || !slices.Contains(m.objects[0].Labels, "refund") {
		t.Fatal("saving a local pair should keep native labels and persist the pair")
	}
	m.search = "label:=area:payments"
	if rows := m.taskRows(); !slices.Equal(rows, []int{0}) {
		t.Fatalf("expected exact key–value label search to find the task, got %v", rows)
	}
}

func TestLiveConnectionResultsDistinguishCancellationFromFailure(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	now := time.Now()
	connection := common.WorkbenchConnection{ID: "obsidian", Kind: common.WorkbenchObsidianTasks, Name: "Vault"}
	var tasks = []sourceObject{{ID: "kept", ConnectionID: connection.ID, Title: "Last confirmed task"}}
	for _, test := range []struct {
		state  string
		paused bool
		status string
	}{
		{"cancelled", false, "同步正常"},
		{"failed", false, "同步失败"},
		{"success", true, "已暂停"},
	} {
		snapshot := common.WorkbenchSnapshot{
			WorkbenchConnection: connection, LastSuccess: &now,
			Attempts: []common.WorkbenchAttempt{{ID: 1, State: test.state}},
		}
		snapshot.Paused = test.paused
		m.applyLive([]common.WorkbenchSnapshot{snapshot}, tasks)
		got := m.connectionByID(connection.ID)
		if got == nil || got.Status != test.status || got.LastConfirmedAt == "" || len(m.objects) != 1 || m.objects[0].ID != "kept" {
			t.Fatalf("%s: connection=%+v, tasks=%+v", test.state, got, m.objects)
		}
	}
}

func TestSyncLogReturnsKeyboardFocusToItsTrigger(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	m.navigate(connectionsPage)
	p := ui.NewTester(m.view, 640, 480)
	label := "同步日志 · 工作 vault"
	click(t, p, label)
	if m.syncLogID == "" {
		t.Fatal("sync log did not open")
	}
	p.Key(0, ui.KeyEscape)
	if m.syncLogID != "" || !p.Focused(label) {
		t.Fatalf("focus after Escape: log=%q, trigger=%t", m.syncLogID, p.Focused(label))
	}
	p.Key(0, ui.KeyEnter)
	if m.syncLogID == "" {
		t.Fatal("keyboard activation did not reopen the sync log")
	}
	click(t, p, "关闭")
	if m.syncLogID != "" || !p.Focused(label) {
		t.Fatal("closing the sync log did not return focus")
	}
}

func TestDisconnectFailedObsidianConnectionFromSettings(t *testing.T) {
	dir := t.TempDir()
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	vault := t.TempDir()
	note := filepath.Join(vault, "Note.md")
	const original = "# Tasks\n- [ ] Keep this note\n"
	if err := os.WriteFile(note, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	remote := services.NewRemote(config.SocketPath(dir))
	created, err := remote.ConfigureWorkbench(common.WorkbenchConnection{
		Kind: common.WorkbenchObsidianTasks, Name: "Broken vault", Vault: vault, Scope: "Missing",
	}, common.WorkbenchBridgeInput{})
	if err != nil {
		t.Fatal(err)
	}
	var failed common.WorkbenchSnapshot
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		failed, err = remote.WorkbenchSyncDetails(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(failed.Attempts) > 0 && failed.Attempts[0].State == "failed" {
			break
		}
	}
	if len(failed.Attempts) == 0 || failed.Attempts[0].State != "failed" {
		t.Fatalf("expected a failed connection: %+v", failed)
	}
	m := newMockup()
	m.useAdaptiveLayout()
	m.live, m.liveBusy = remote, true
	m.applyLive([]common.WorkbenchSnapshot{failed}, nil)
	m.navigate(connectionsPage)
	p := ui.NewTester(m.view, 680, 580)
	click(t, p, "连接配置 · Broken vault", "解除连接")
	if m.removeConnectionID != created.ID {
		t.Fatalf("disconnect confirmation did not open: %q, editor open: %v", m.removeConnectionID, m.connectorEditor != nil)
	}
	p.Frame()
	click(t, p, "保留连接")
	if _, err := remote.WorkbenchSyncDetails(created.ID); err != nil {
		t.Fatalf("cancelling deleted the connection: %v", err)
	}
	click(t, p, "解除连接", "确认解除连接")
	if m.connectorEditor != nil {
		t.Fatal("disconnect should close connection settings")
	}
	if _, err := remote.WorkbenchSyncDetails(created.ID); err == nil || common.ProtocolError(err).Code != "NOT_FOUND" {
		t.Fatalf("connection was not removed: %v", err)
	}
	if data, err := os.ReadFile(note); err != nil || string(data) != original {
		t.Fatalf("source note was modified: %v", err)
	}
}

func TestLiveSyncDisplaysReadCountsWhenAvailable(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	conn := &connection{ID: "reading"}
	m.pendingSync[conn.ID] = true
	view := func(c *ui.Context) { m.syncProgressView(c, conn) }
	if !ui.NewTester(view, 490, 100).HasText("读取来源") {
		t.Fatal("missing indeterminate read status")
	}
	conn.ReadProgress = &common.WorkbenchReadProgress{Read: 64, Total: 4000}
	if !ui.NewTester(view, 490, 100).HasText("已读取 64 / 4000 篇笔记") {
		t.Fatal("missing live note count")
	}
}

func TestObsidianWriteUsesCLIUnlessRESTConfigured(t *testing.T) {
	m := newMockup()
	edit := &connectorEdit{connection: connection{ReadMethod: common.WorkbenchObsidianCLI}}
	p := ui.NewTester(func(c *ui.Context) { m.obsidianBridgeFields(c, edit) }, 460, 240)
	click(t, p, "允许回写")
	if !edit.WriteEnabled || p.HasText("桥接地址") || p.HasText("桥接令牌") || p.HasText("桥接证书") {
		t.Fatal("CLI should allow writeback without REST settings")
	}
	edit.ReadMethod = ""
	p.Frame()
	if p.HasText("桥接地址") {
		t.Fatal("Markdown reads should allow CLI writeback without REST settings")
	}
	click(t, p, "配置 REST 桥接…")
	p.Frame()
	if !p.HasText("桥接地址") {
		t.Fatal("explicitly configuring a REST bridge should show its settings")
	}
}
