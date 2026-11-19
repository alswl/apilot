package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/alswl/apilot/pkg/integrations"
)

func (m *mockup) loadTaskViewStore(path string) error {
	m.taskViewStorePath = path
	views, err := loadNamedTaskViews(path)
	if err == nil {
		m.savedTaskViews = views
	}
	return err
}

func loadNamedTaskViews(path string) ([]savedTaskView, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var views []savedTaskView
	if err := json.Unmarshal(data, &views); err != nil {
		return nil, err
	}
	return views, nil
}

func (m *mockup) saveNamedTaskView(view savedTaskView) error {
	path := m.taskViewStorePath
	if path == "" {
		m.savedTaskViews = append(m.savedTaskViews, view)
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := integrations.LockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()

	views, err := loadNamedTaskViews(path)
	if err != nil {
		return err
	}
	views = append(views, view)
	tmp, err := os.CreateTemp(dir, ".task-views-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	writeErr := json.NewEncoder(tmp).Encode(views)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	m.savedTaskViews = views
	return nil
}
