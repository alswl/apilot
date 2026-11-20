package services

import "github.com/alswl/apilot/core/pkg/common"

type AutomationSnapshot struct {
	Tasks     []common.Task           `json:"tasks"`
	Runs      []common.Run            `json:"runs"`
	Observers []common.Observer       `json:"observers"`
	Events    []common.AttentionEvent `json:"events"`
	Status    map[string]any          `json:"status"`
	Settings  common.Settings         `json:"settings"`
}

func (s *Service) AutomationSnapshot() (AutomationSnapshot, error) {
	events, err := s.Events()
	if err != nil {
		return AutomationSnapshot{}, err
	}
	return AutomationSnapshot{Tasks: s.Tasks(), Runs: s.Runs(""), Observers: s.Observers(), Events: events, Status: s.Status(), Settings: s.Settings()}, nil
}

type AutomationRemote struct{ *Remote }

func (r AutomationRemote) AutomationSnapshot() (AutomationSnapshot, error) {
	return remoteResult[AutomationSnapshot](r.Remote, "automation.snapshot", nil)
}

func (r AutomationRemote) SaveTask(id string, in common.TaskInput, version int64) (common.Task, error) {
	if id == "" {
		return remoteResult[common.Task](r.Remote, "task.create", in)
	}
	return remoteResult[common.Task](r.Remote, "task.update", map[string]any{"task": id, "patch": in, "expected_revision": version})
}

func (r AutomationRemote) DeleteTask(id string) error {
	_, err := r.Call("task.remove", map[string]string{"task": id})
	return err
}

func (r AutomationRemote) SetAutomatic(id string, on bool, version int64) (common.Task, error) {
	return remoteResult[common.Task](r.Remote, "automation.set_automatic", map[string]any{"id": id, "enabled": on, "revision": version})
}

func (r AutomationRemote) RunTask(id, trigger string) (common.Run, error) {
	return remoteResult[common.Run](r.Remote, "automation.run", map[string]string{"id": id, "trigger": trigger})
}

func (r AutomationRemote) StopRun(id string) (common.Run, error) {
	return remoteResult[common.Run](r.Remote, "run.stop", map[string]string{"run_id": id})
}

func (r AutomationRemote) Logs(id, stream string, offset int64, limit int) (common.LogChunk, error) {
	return remoteResult[common.LogChunk](r.Remote, "logs.read", map[string]any{"run_id": id, "stream": stream, "offset": offset, "limit": limit})
}

func (r AutomationRemote) SaveObserver(id string, in common.ObserverInput, version int64) (common.Observer, error) {
	if id == "" {
		return remoteResult[common.Observer](r.Remote, "observer.create", in)
	}
	return remoteResult[common.Observer](r.Remote, "observer.update", map[string]any{"observer": id, "patch": in, "expected_revision": version})
}

func (r AutomationRemote) DeleteObserver(id string) error {
	_, err := r.Call("observer.remove", map[string]string{"observer": id})
	return err
}

func (r AutomationRemote) SetObserverEnabled(id string, on bool) (common.Observer, error) {
	return remoteResult[common.Observer](r.Remote, "observer.set_enabled", map[string]any{"observer": id, "enabled": on})
}

func (r AutomationRemote) AckEvent(id string) (common.AttentionEvent, error) {
	return remoteResult[common.AttentionEvent](r.Remote, "event.ack", map[string]string{"event_id": id})
}

func (r AutomationRemote) SaveSettings(value common.Settings) (common.Settings, error) {
	return remoteResult[common.Settings](r.Remote, "settings.update", map[string]any{"patch": value, "expected_revision": value.Revision})
}

func (r AutomationRemote) AllowNotification(key string) (bool, error) {
	return remoteResult[bool](r.Remote, "automation.notification.claim", map[string]string{"id": key})
}

func (r AutomationRemote) Suspend() { _, _ = r.Call("automation.suspend", nil) }
func (r AutomationRemote) Resume()  { _, _ = r.Call("automation.resume", nil) }
