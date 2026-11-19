package server

import (
	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchWorkbench(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "workbench.task.list":
		var q common.WorkbenchQuery
		if err := decode(r.Params, &q); err != nil {
			return nil, err
		}
		return s.WorkbenchTasks(q)
	case "workbench.task.show", "workbench.sync.show":
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(r.Params, &p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			return nil, common.Err("INVALID_ARGUMENT", "ID is required")
		}
		if r.Method == "workbench.task.show" {
			return s.WorkbenchTask(p.ID)
		}
		return s.WorkbenchSnapshot(p.ID)
	case "workbench.connection.list":
		items, err := s.WorkbenchConnections()
		return map[string]any{"items": items}, err
	default:
		return nil, common.Err("INVALID_ARGUMENT", "Unknown workbench method")
	}
}
