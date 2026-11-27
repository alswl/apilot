package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type taskQueryChip struct {
	Label      string `json:"label"`
	Expression string `json:"expression"`
}

type taskQueryView struct {
	Engine       string                       `json:"queryEngine"`
	Environment  int                          `json:"environmentVersion,omitempty"`
	Keyword      string                       `json:"keyword,omitempty"`
	Expression   string                       `json:"expression,omitempty"`
	ChipBase     string                       `json:"chipBase,omitempty"`
	Chips        []taskQueryChip              `json:"chips,omitempty"`
	Widths       map[string]float32           `json:"listWidths,omitempty"`
	Dependencies []string                     `json:"fieldDependencies,omitempty"`
	List         common.WorkbenchQueryDisplay `json:"list"`
	Board        common.WorkbenchQueryDisplay `json:"board"`
}

type taskQueryConfig struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Revision      int64                        `json:"revision"`
	Timezone      string                       `json:"timezone"`
	Fields        []common.WorkbenchQueryField `json:"fields"`
	Views         []savedTaskView              `json:"views"`
}

var errTaskQueryConfigChanged = errors.New("task view configuration changed")

func workspaceTimezone() string {
	if zone, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(zone, "zoneinfo/"); ok {
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	name := time.Local.String()
	if _, err := time.LoadLocation(name); err == nil && name != "Local" {
		return name
	}
	return "UTC"
}

func readTaskQueryConfig(path string) (taskQueryConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return taskQueryConfig{}, err
	}
	var cfg taskQueryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.SchemaVersion != 2 || cfg.Revision < 1 || cfg.Timezone == "" {
		return cfg, fmt.Errorf("%s: invalid task view schema or revision", path)
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil || cfg.Timezone == "Local" {
		return cfg, fmt.Errorf("%s: invalid workspace timezone", path)
	}
	ids := make(map[string]bool, len(cfg.Views))
	for _, view := range cfg.Views {
		if view.ID == "" || ids[view.ID] || view.Query == nil || (view.Query.Engine != "legacy-v1" && view.Query.Engine != "cel") {
			return cfg, fmt.Errorf("%s: invalid saved view %q", path, view.Name)
		}
		if view.Query.Engine == "cel" && view.Query.Environment != 1 {
			return cfg, fmt.Errorf("%s: unsupported CEL environment for %q", path, view.Name)
		}
		ids[view.ID] = true
	}

	return cfg, nil
}

func writeTaskQueryConfig(path string, cfg taskQueryConfig) (bool, error) {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".task-views-*.json")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if err = json.NewEncoder(file).Encode(cfg); err == nil {
		err = file.Sync()
	}
	if err := errors.Join(err, file.Close()); err != nil {
		return false, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return false, err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return true, err
	}
	return true, errors.Join(parent.Sync(), parent.Close())
}

func bootTaskQueryConfig(path string) (taskQueryConfig, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return taskQueryConfig{}, err
	}
	lock, err := integrations.LockFile(path + ".lock")
	if err != nil {
		return taskQueryConfig{}, err
	}
	defer lock.Close()
	cfg, err := readTaskQueryConfig(path)
	if !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	views, err := loadNamedTaskViews(filepath.Join(dir, "task-views.json"))
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", filepath.Join(dir, "task-views.json"), err)
	}
	for i := range views {
		views[i].Query = &taskQueryView{Engine: "legacy-v1"}
	}
	cfg = taskQueryConfig{SchemaVersion: 2, Revision: 1, Timezone: workspaceTimezone(), Fields: []common.WorkbenchQueryField{}, Views: append([]savedTaskView{}, views...)}
	if _, err := writeTaskQueryConfig(path, cfg); err != nil {
		return taskQueryConfig{}, err
	}
	return cfg, nil
}

func (m *mockup) adoptTaskQueryConfig(cfg taskQueryConfig) {
	m.customQuery.config = cfg
	m.savedTaskViews = cfg.Views
}

func (m *mockup) updateTaskQueryConfig(change func(*taskQueryConfig) error) error {
	q := &m.customQuery
	if q.path == "" {
		return errors.New("task view storage unavailable")
	}
	lock, err := integrations.LockFile(q.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	cfg, err := readTaskQueryConfig(q.path)
	if err != nil {
		return err
	}
	if cfg.Revision != q.config.Revision {
		m.adoptTaskQueryConfig(cfg)
		return errTaskQueryConfigChanged
	}
	if err := change(&cfg); err != nil {
		return err
	}
	if err := validateTaskQueryReferences(cfg); err != nil {
		return err
	}
	cfg.Revision++
	replaced, err := writeTaskQueryConfig(q.path, cfg)
	if err == nil {
		m.adoptTaskQueryConfig(cfg)
	} else if replaced {
		if actual, readErr := readTaskQueryConfig(q.path); readErr == nil {
			m.adoptTaskQueryConfig(actual)
			if actual.Revision == cfg.Revision {
				return nil
			}
		}
	}
	return err
}

func validateTaskQueryReferences(cfg taskQueryConfig) error {
	fields := make(map[string]bool)
	for _, field := range cfg.Fields {
		fields[field.ID] = true
	}
	var problems []string
	for _, view := range cfg.Views {
		if view.Query == nil || view.Query.Engine != "cel" {
			continue
		}
		ids := slices.Clone(view.Query.Dependencies)
		for _, display := range []common.WorkbenchQueryDisplay{view.Query.List, view.Query.Board} {
			ids = append(ids, display.Fields...)
			ids = append(ids, display.GroupBy, display.ColumnBy, display.SwimlaneBy, display.SortBy, display.SumBy, display.SliceBy)
		}
		for _, id := range ids {
			if id != "" && !strings.HasPrefix(id, "task.") && !strings.HasPrefix(id, "native.") && !fields[id] {
				problem := fmt.Sprintf("%s · %s", view.Name, id)
				if !slices.Contains(problems, problem) {
					problems = append(problems, problem)
				}
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(tr("字段仍被视图使用") + "\n" + strings.Join(problems, "\n"))
	}
	return nil
}

func putTaskQueryView(cfg *taskQueryConfig, view savedTaskView) error {
	name := strings.TrimSpace(view.Name)
	if name == "" {
		return errors.New(tr("请输入视图名称"))
	}
	for _, existing := range cfg.Views {
		if existing.ID != view.ID && strings.EqualFold(strings.TrimSpace(existing.Name), name) {
			old := slices.IndexFunc(cfg.Views, func(candidate savedTaskView) bool { return candidate.ID == view.ID })
			if old < 0 || cfg.Views[old].Name != view.Name {
				return errors.New(tr("视图名称已存在"))
			}
		}
	}
	view.Name = name
	cfg.Views = upsertNamedTaskView(cfg.Views, view)
	return nil
}
