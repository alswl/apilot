package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func registerWorkbenchSyncRunHTTP(api huma.API, s *services.Service) {
	registerWorkbench(api, http.MethodGet, workbenchPath+"/sync-runs", "listWorkbenchSyncRuns", "List scope sync runs", "Sync", []int{400},
		func(in *common.WorkbenchSyncQuery) (workbenchItems[common.WorkbenchSyncRun], error) {
			items, err := s.WorkbenchSyncRuns(*in)
			return workbenchItems[common.WorkbenchSyncRun]{items}, err
		})
	registerWorkbench(api, http.MethodGet, workbenchPath+"/sync-runs/{id}", "getWorkbenchSyncRun", "Read a sync run by ID", "Sync", []int{404},
		func(in *workbenchID) (common.WorkbenchSyncRun, error) { return s.WorkbenchSyncRun(in.ID) })
	registerWorkbench(api, http.MethodGet, workbenchPath+"/sync-usage", "listWorkbenchSyncUsage", "Summarize sync usage by scope and mode", "Sync", []int{400},
		func(in *common.WorkbenchSyncQuery) (workbenchItems[common.WorkbenchSyncUsage], error) {
			items, err := s.WorkbenchSyncUsage(*in)
			return workbenchItems[common.WorkbenchSyncUsage]{items}, err
		})
}
