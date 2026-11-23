package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func dispatchWorkbench(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "workbench.operation.submit":
		var input common.WorkbenchTaskWrite
		if err := decode(r.Params, &input); err != nil {
			return nil, err
		}
		return s.SubmitWorkbenchTaskOperation(input)
	case "workbench.operation.show", "workbench.operation.reconcile":
		var input struct {
			ID string `json:"id"`
		}
		if err := decode(r.Params, &input); err != nil {
			return nil, err
		}
		if input.ID == "" {
			return nil, common.Err("INVALID_ARGUMENT", "Request ID is required")
		}
		if r.Method == "workbench.operation.show" {
			return s.WorkbenchTaskOperationReceipt(input.ID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.ReconcileWorkbenchTaskOperation(ctx, input.ID)
	case "workbench.connector.list":
		items, err := s.WorkbenchConnectors()
		return map[string]any{"items": items}, err
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
			snapshot, err := s.WorkbenchSnapshot(p.ID)
			return publicWorkbenchSnapshot(snapshot), err
		}
	case "workbench.connection.save":
		var c common.WorkbenchConnection
		if err := decode(r.Params, &c); err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(r.Params, &fields); err != nil {
			return nil, common.Err("INVALID_ARGUMENT", "Invalid connection input")
		}
		if _, present := fields["read_method"]; !present && c.ID != "" {
			prior, err := s.WorkbenchSnapshot(c.ID)
			if err != nil {
				return nil, err
			}
			c.ReadMethod = prior.ReadMethod
		}
		id, err := s.SaveWorkbench(c)
		if err != nil {
			return nil, err
		}
		snapshot, err := s.WorkbenchSnapshot(id)
		return publicWorkbenchSnapshot(snapshot), err
	case "workbench.task.import":
		var batch common.WorkbenchImport
		if err := decode(r.Params, &batch); err != nil {
			return nil, err
		}
		return s.ImportWorkbench(batch)
	case "workbench.connection.list":
		items, err := s.WorkbenchConnections()
		for i := range items {
			items[i] = publicWorkbenchSnapshot(items[i])
		}
		return map[string]any{"items": items}, err
	default:
		return nil, common.Err("INVALID_ARGUMENT", "Unknown workbench method")
	}
}
