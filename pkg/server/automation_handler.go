package server

import (
	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchAutomation(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "automation.snapshot", "automation.suspend", "automation.resume":
		if err := decode(r.Params, &struct{}{}); err != nil {
			return nil, err
		}
		switch r.Method {
		case "automation.snapshot":
			return s.AutomationSnapshot()
		case "automation.suspend":
			s.Suspend()
		case "automation.resume":
			s.Resume()
		}
		return true, nil
	case "automation.set_automatic":
		var p struct {
			ID       string `json:"id"`
			Enabled  bool   `json:"enabled"`
			Revision int64  `json:"revision"`
		}
		if err := decode(r.Params, &p); err != nil {
			return nil, err
		}
		return s.SetAutomatic(p.ID, p.Enabled, p.Revision)
	case "automation.run", "automation.notification.claim":
		var p struct {
			ID      string `json:"id"`
			Trigger string `json:"trigger,omitempty"`
		}
		if err := decode(r.Params, &p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			return nil, common.Err("INVALID_ARGUMENT", "An automation ID is required")
		}
		if r.Method == "automation.notification.claim" {
			return s.AllowNotification(p.ID)
		}
		if p.Trigger != "manual" {
			return nil, common.Err("INVALID_ARGUMENT", "Expected manual trigger")
		}
		return s.RunTask(p.ID, "manual")
	default:
		return nil, common.Err("INVALID_ARGUMENT", "Unknown automation method")
	}
}
