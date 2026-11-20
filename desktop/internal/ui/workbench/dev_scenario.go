package workbench

import (
	"fmt"

	"github.com/alswl/apilot/core/pkg/common"
)

const taskCreateScenario = "task-create-pending"

type devTaskScenario struct {
	requests []common.WorkbenchTaskWrite
}

func newDevTaskScenario() *mockup {
	m := newFramework()
	m.devTask = &devTaskScenario{}
	m.connectionsData = []connection{{ID: "dev-local", Name: "Local", SourceKind: common.WorkbenchLocal, Status: "可用", Writable: true, Version: 1}}
	m.page = tasksPage
	m.newTaskOpen, m.newTaskPending = true, true
	m.newTaskDraft, m.newTaskSource = "Write once", "Local"
	m.newTaskRequest = common.WorkbenchTaskWrite{RequestID: "dev-create-once", ConnectionID: "dev-local", ConnectionVersion: 1, Title: m.newTaskDraft}
	m.devTask.requests = append(m.devTask.requests, m.newTaskRequest)
	return m
}

func (m *mockup) resolveDevTask(state string) {
	m.finishLiveTaskCreate(common.WorkbenchTaskReceipt{SyncState: state}, nil)
	if state != "confirmed" {
		return
	}
	snapshot := common.WorkbenchSnapshot{
		WorkbenchConnection: common.WorkbenchConnection{ID: "dev-local", Name: "Local", Kind: common.WorkbenchLocal},
		Capabilities:        common.WorkbenchCapabilities{Read: true, Create: true},
	}
	task := sourceObject{ID: "dev-task", ConnectionID: "dev-local", Kind: common.WorkbenchLocal, Title: m.newTaskDraft, State: "open"}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
}

func RunDevScenario(name string) error {
	if name != taskCreateScenario {
		return fmt.Errorf("unknown development scenario %q; available: %s", name, taskCreateScenario)
	}
	return runWorkspace(newDevTaskScenario(), "", "", "")
}
