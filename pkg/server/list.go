package server

import (
	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchList(s *services.Service, r Request) (any, error) {
	var p struct {
		Task   string `json:"task"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
		Unread bool   `json:"unread"`
	}
	if e := decode(r.Params, &p); e != nil {
		return nil, e
	}
	if p.Limit < 0 || p.Limit > 1000 || p.Offset < 0 {
		return nil, common.Err("INVALID_ARGUMENT", "Invalid pagination")
	}
	if r.Method == "task.list" {
		return page(s.Tasks(), p.Limit, p.Offset), nil
	}
	if r.Method == "run.list" {
		return page(s.Runs(p.Task), p.Limit, p.Offset), nil
	}
	events, e := s.Events()
	if p.Unread {
		filtered := []common.AttentionEvent{}
		for _, v := range events {
			if v.AcknowledgedAt == nil {
				filtered = append(filtered, v)
			}
		}
		events = filtered
	}
	return page(events, p.Limit, p.Offset), e
}
