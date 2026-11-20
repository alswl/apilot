package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/server"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestSyncUsageUsesCompletedRetainedAttempts(t *testing.T) {
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	finish := start.Add(250 * time.Millisecond)
	snapshots := []common.WorkbenchSnapshot{
		{WorkbenchConnection: common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: common.WorkbenchObsidianTasks}, Attempts: []common.WorkbenchAttempt{
			{Trigger: "source_changed", State: "success", Scope: common.WorkbenchReadNotes, Started: start, Finished: &finish},
			{Trigger: "manual", State: "failed", Scope: common.WorkbenchReadFull},
			{Trigger: "write", State: "success", Scope: common.WorkbenchReadNotes},
			{Trigger: "manual", State: "running", Scope: common.WorkbenchReadFull},
			{Trigger: "manual", State: "superseded", Scope: common.WorkbenchReadFull},
		}},
		{WorkbenchConnection: common.WorkbenchConnection{ID: "github", Name: "GitHub", Kind: common.WorkbenchGitHubIssues}, Attempts: []common.WorkbenchAttempt{{Trigger: "startup", State: "success"}}},
		{WorkbenchConnection: common.WorkbenchConnection{ID: "local", Kind: common.WorkbenchLocal}, Attempts: []common.WorkbenchAttempt{{Trigger: "import", State: "success"}}},
	}
	got := summarizeSyncUsage(snapshots)
	if len(got) != 2 || got[0].attempts != 2 || got[0].failures != 1 || got[0].full != 1 || got[0].notes != 1 || got[0].successDuration != 250*time.Millisecond || got[0].successDurationCount != 1 {
		t.Fatalf("vault summary: %+v", got)
	}
	if got[1].attempts != 1 || got[1].full != 0 || got[1].notes != 0 || got[1].successDurationCount != 0 {
		t.Fatalf("legacy attempt should not invent scope or duration: %+v", got[1])
	}
	m := newFramework()
	m.live = services.NewRemote("")
	m.liveConnections = snapshots
	m.useAdaptiveLayout()
	m.page, m.usageTab = usagePage, 2
	preview := ui.NewTester(m.view, 680, 580)
	for _, label := range []string{"每个连接最近 20 条记录中的已完成同步", "按连接与读取范围", "按连接失败次数", "按连接的成功平均耗时"} {
		if !preview.HasText(label) {
			t.Fatalf("missing live usage label %q", label)
		}
	}
	previousLanguage := englishUI.Swap(true)
	t.Cleanup(func() { englishUI.Store(previousLanguage) })
	for _, size := range [][2]int{{680, 580}, {1190, 730}} {
		preview.SetSize(size[0], size[1])
		if !preview.HasText("Read scope by connection") || !preview.HasText("Average successful duration by connection") {
			t.Fatalf("missing English sync metrics at %v", size)
		}
	}
}

func TestSyncUsageReadsActualConnectionHistory(t *testing.T) {
	dir, vault := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "tasks.md"), []byte("- [ ] Task\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	id, err := runtime.Service.SaveWorkbench(common.WorkbenchConnection{Name: "Real Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	remote := services.NewRemote(config.SocketPath(dir))
	deadline := time.Now().Add(5 * time.Second)
	var snapshots []common.WorkbenchSnapshot
	for time.Now().Before(deadline) {
		snapshots, err = remote.WorkbenchConnections()
		if err != nil {
			t.Fatal(err)
		}
		for _, snapshot := range snapshots {
			if snapshot.ID == id && len(summarizeSyncUsage([]common.WorkbenchSnapshot{snapshot})) > 0 {
				m := newFramework()
				m.live, m.liveConnections = remote, snapshots
				m.useAdaptiveLayout()
				m.page, m.usageTab = usagePage, 2
				preview := ui.NewTester(m.view, 680, 580)
				if !preview.HasText("Real Vault · 全范围") || !preview.HasText("同步次数") {
					t.Fatal("actual connection history was not rendered")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no completed sync through remote API: %+v", snapshots)
}

func TestSyncUsageEmptyAndUnavailable(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.useAdaptiveLayout()
	m.page, m.usageTab = usagePage, 2
	preview := ui.NewTester(m.view, 680, 580)
	if !preview.HasText("暂无同步记录") {
		t.Fatal("missing empty sync state")
	}
	click(t, preview, "查看连接器")
	if m.page != connectionsPage {
		t.Fatal("empty state did not open connections")
	}
	m.page, m.liveError = usagePage, true
	preview.Frame()
	if !preview.HasText("无法读取同步数据") {
		t.Fatal("missing unavailable sync state")
	}
}
