package server

import (
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func dispatchWorkbench(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "workbench.connector.list":
		return map[string]any{"items": services.WorkbenchConnectors()}, nil
	case "workbench.task.list":
		var q common.WorkbenchQuery
		if err := decode(r.Params, &q); err != nil {
			return nil, err
		}
		return s.WorkbenchTasks(q)
	case "workbench.task.show", "workbench.sync.show", "workbench.connection.show", "workbench.sync.run", "workbench.connection.remove":
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
		switch r.Method {
		case "workbench.sync.run":
			return map[string]string{"id": p.ID}, s.SyncWorkbench(p.ID)
		case "workbench.connection.remove":
			return map[string]string{"id": p.ID}, s.RemoveWorkbench(p.ID)
		default:
			return s.WorkbenchSnapshot(p.ID)
		}
	case "workbench.connection.save":
		var c common.WorkbenchConnection
		if err := decode(r.Params, &c); err != nil {
			return nil, err
		}
		id, err := s.SaveWorkbench(c)
		if err != nil {
			return nil, err
		}
		return s.WorkbenchSnapshot(id)
	case "workbench.task.import":
		var batch common.WorkbenchImport
		if err := decode(r.Params, &batch); err != nil {
			return nil, err
		}
		return s.ImportWorkbench(batch)
	case "workbench.connection.list":
		items, err := s.WorkbenchConnections()
		return map[string]any{"items": items}, err
	default:
		return nil, common.Err("INVALID_ARGUMENT", "Unknown workbench method")
	}
}
