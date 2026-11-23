package workbench

import (
	"slices"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/egoist/mygo/ui"
)

type devEventScenario struct {
	snapshots []common.WorkbenchSnapshot
	tasks     []common.WorkbenchTask
	sequence  uint64
	fail      bool
}

func newDevEventScenario() *mockup {
	m := newFramework()
	m.devEvents = &devEventScenario{
		snapshots: []common.WorkbenchSnapshot{
			{WorkbenchConnection: common.WorkbenchConnection{ID: "dev-local", Kind: common.WorkbenchLocal, Name: "Local", Version: 1}},
			{WorkbenchConnection: common.WorkbenchConnection{ID: "dev-github", Kind: common.WorkbenchGitHubRepository, Name: "GitHub", Repository: "fixture/issues", Version: 1, AccessState: "available"}},
		},
		tasks: []common.WorkbenchTask{
			{ID: "dev-task", ConnectionID: "dev-local", Kind: common.WorkbenchLocal, Title: "Local task", State: "open"},
			{ID: "dev-issue", ConnectionID: "dev-github", Kind: common.WorkbenchGitHubRepository, Title: "GitHub fixture", State: "open"},
		},
	}
	m.page = tasksPage
	m.devEvent("snapshot")
	return m
}

func (m *mockup) devEvent(kind string) {
	fixture := m.devEvents
	fixture.sequence++
	if kind == "changed" && m.liveDisconnected {
		return
	}
	m.receiveLiveEvent(common.WorkbenchEvent{Instance: "fixture", Sequence: fixture.sequence, Type: kind})
}

func (f *devEventScenario) read(m *mockup) {
	m.liveError = f.fail
	m.liveStale = f.fail || m.liveDisconnected
	if f.fail {
		return
	}
	m.clearChangedGitHubAccess(f.snapshots)
	m.liveConnections = slices.Clone(f.snapshots)
	m.applyLive(f.snapshots, slices.Clone(f.tasks))
}

func (m *mockup) devEventControls(c *ui.Context) {
	f := m.devEvents
	ui.Row(c).Gap(4).Children(func() {
		if ui.Button(c.Key("change"), tr("外部更新")).Clicked() {
			if f.tasks[0].State == "open" {
				f.tasks[0].State = "done"
			} else {
				f.tasks[0].State = "open"
			}
			m.devEvent("changed")
		}
		if ui.Button(c.Key("disconnect"), tr("连接断开")).Clicked() {
			m.receiveLiveDisconnect()
		}
		if ui.Button(c.Key("failure"), tr("刷新失败")).Clicked() {
			f.fail = true
			m.devEvent("changed")
		}
		if ui.Button(c.Key("reconnect"), tr("重连")).Clicked() {
			f.fail = false
			m.devEvent("snapshot")
		}
		if ui.Button(c.Key("revoke"), tr("撤回访问权")).Clicked() {
			f.snapshots[1].AccessState = "unavailable"
			f.snapshots[1].Version++
			m.devEvent("changed")
		}
	})
}
