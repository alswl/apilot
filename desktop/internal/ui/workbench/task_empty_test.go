package workbench

import (
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestLiveTaskEmptyStatesAndRecovery(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.useAdaptiveLayout()
	m.navigate(tasksPage)
	preview := ui.NewTester(m.view, 680, 580)
	if !preview.HasText("正在读取任务…") {
		t.Fatal("initial load must not be presented as an empty source")
	}
	local := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "local", Name: "Local", Kind: common.WorkbenchLocal}}
	vault := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: obsidianTasks}}
	m.applyLive([]common.WorkbenchSnapshot{local}, nil)
	preview.Frame()
	if !preview.HasText("添加连接后，来源任务会显示在这里。") {
		t.Fatal("default Local connection should not hide the source onboarding guidance")
	}
	click(t, preview, "添加连接")
	if m.page != connectionsPage || !preview.HasText("返回任务") {
		t.Fatal("the onboarding action should open connections with a return action")
	}
	m.addConnector(obsidianTasks)
	preview.Frame()
	preview.Key(0, ui.KeyEscape)
	if m.connectorEditor != nil || m.page != connectionsPage {
		t.Fatal("Escape should close the connector editor before leaving connections")
	}
	preview.Key(0, ui.KeyEscape)
	if m.page != tasksPage || !preview.Focused("添加连接") {
		t.Fatal("Escape should restore the task list and focus the onboarding action")
	}
	m.applyLive([]common.WorkbenchSnapshot{local, vault}, nil)
	preview.Frame()
	if !preview.HasText("来源尚未完成首次同步。") {
		t.Fatal("an unread source should not look confirmed empty")
	}
	m.pendingSync["vault"] = true
	preview.Frame()
	if !preview.HasText("来源正在同步，任务确认后会显示在这里。") {
		t.Fatal("a running sync should be visible")
	}
	delete(m.pendingSync, "vault")
	m.connectionByID("vault").Status = "同步正常"
	preview.Frame()
	if !preview.HasText("已确认的来源中没有任务。") {
		t.Fatal("a confirmed empty source should be distinct from loading")
	}
	m.search = "missing"
	preview.Frame()
	if !preview.HasText("没有符合条件的任务") {
		t.Fatal("filters should be visible even when there are no tasks")
	}
	click(t, preview, "清除筛选")
	if m.search != "" || !preview.HasText("已确认的来源中没有任务。") {
		t.Fatal("clearing the filter should reveal the source state")
	}
	m.connectionByID("vault").Status = "同步失败"
	preview.Frame()
	if !preview.HasText("部分来源无法同步，请检查连接状态。") {
		t.Fatal("a failed source must not be presented as empty")
	}
	click(t, preview, "查看连接器")
	if m.page != connectionsPage {
		t.Fatal("recovery should open connections")
	}
	m.navigate(tasksPage)
	m.liveError = true
	preview.Frame()
	if !preview.HasText("暂时无法确认任务列表，请检查连接。") {
		t.Fatal("a failed workbench read should not be shown as a confirmed source result")
	}
}

func TestLiveTaskListShowsPartialSourceFailure(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.useAdaptiveLayout()
	m.navigate(tasksPage)
	vault := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: obsidianTasks}}
	m.applyLive([]common.WorkbenchSnapshot{vault}, []sourceObject{{ID: "task", ConnectionID: "vault", Title: "Retained task", State: "open"}})
	m.connectionByID("vault").Status = "同步失败"
	preview := ui.NewTester(m.view, 1190, 730)
	if !preview.HasText("Retained task") || !preview.HasText("部分来源无法同步，列表可能不完整。") {
		t.Fatal("keep confirmed tasks visible and disclose incomplete results")
	}
	m.search = "not found"
	preview.Frame()
	if !preview.HasText("部分来源无法同步，列表可能不完整。") || !preview.HasText("没有符合条件的任务") {
		t.Fatal("filtered empty results must disclose an unavailable source")
	}
}

func TestTaskConnectionReturnFocusWhenTasksArrive(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.useAdaptiveLayout()
	m.navigate(tasksPage)
	local := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "local", Name: "Local", Kind: common.WorkbenchLocal}}
	m.applyLive([]common.WorkbenchSnapshot{local}, nil)
	preview := ui.NewTester(m.view, 680, 580)
	click(t, preview, "添加连接")
	m.applyLive([]common.WorkbenchSnapshot{local}, []sourceObject{{ID: "task", ConnectionID: "local", Title: "New task", State: "open"}})
	preview.Frame()
	preview.Key(0, ui.KeyEscape)
	if m.page != tasksPage || !preview.Focused("搜索任务") || m.restoreConnectionEntryFocus {
		t.Fatal("when the entry disappears, return should focus search without leaving a pending focus request")
	}
}

func TestLiveTaskEmptyStateEnglish(t *testing.T) {
	previous := englishUI.Swap(true)
	t.Cleanup(func() { englishUI.Store(previous) })
	m := newFramework()
	m.live = services.NewRemote("")
	m.useAdaptiveLayout()
	m.navigate(tasksPage)
	m.applyLive([]common.WorkbenchSnapshot{{WorkbenchConnection: common.WorkbenchConnection{ID: "local", Name: "Local", Kind: common.WorkbenchLocal}}}, nil)
	for _, size := range [][2]int{{680, 580}, {1190, 730}} {
		preview := ui.NewTester(m.view, size[0], size[1])
		if !preview.HasText("Add a connection to see tasks from a source.") || !preview.HasText("Add connection") {
			t.Fatalf("missing English onboarding guidance at %v", size)
		}
	}
}
