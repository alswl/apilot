package workbench

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
)

var pageLocationIDs = [...]string{
	nowPage:         "now",
	tasksPage:       "tasks",
	connectionsPage: "connections",
	scheduledPage:   "scheduled",
	usagePage:       "usage",
}

type workbenchLocation struct {
	Page     string `json:"page"`
	TaskView string `json:"task_view,omitempty"`
}

func (m *mockup) loadHostedNavigation(dataDir string) {
	m.customQuery.path = filepath.Join(dataDir, "task-views.v2.json")
	if cfg, err := bootTaskQueryConfig(m.customQuery.path); err != nil {
		m.customQuery.problem = err.Error()
		log.Printf("Could not load saved task views: %v", err)
	} else {
		m.adoptTaskQueryConfig(cfg)
	}
	m.restoreLocation(filepath.Join(dataDir, "workbench-location.json"))
}

func (m *mockup) restoreLocation(path string) {
	// Restore once during workspace construction, before any explicit action.
	// Enable writes afterward so loading state never rewrites the file.
	defer func() { m.locationPath = path }()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("Could not read Workbench location: %v", err)
		}
		return
	}
	var location workbenchLocation
	if err := json.Unmarshal(data, &location); err != nil {
		log.Printf("Could not decode Workbench location: %v", err)
		return
	}
	page := slices.Index(pageLocationIDs[:], location.Page)
	if page < 0 || location.Page == "" {
		return
	}

	// Apply the browse view while Tasks is active so its layout is available
	// even when the remembered top-level destination is another page.
	if m.hasTaskView(location.TaskView) {
		m.selectTaskView(location.TaskView)
	}
	m.navigate(page)
}

func (m *mockup) hasTaskView(id string) bool {
	switch id {
	case "", "in-progress", "pending", "watching":
		return true
	}
	return slices.ContainsFunc(m.savedTaskViews, func(view savedTaskView) bool { return view.ID == id })
}

func (m *mockup) rememberLocation() {
	if m.locationPath == "" || m.page < 0 || m.page >= len(pageLocationIDs) || pageLocationIDs[m.page] == "" {
		return
	}
	location := workbenchLocation{Page: pageLocationIDs[m.page], TaskView: m.taskView}
	if err := writeWorkbenchLocation(m.locationPath, location); err != nil {
		log.Printf("Could not save Workbench location: %v", err)
	}
}

func writeWorkbenchLocation(path string, location workbenchLocation) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".workbench-location-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	writeErr := json.NewEncoder(file).Encode(location)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
