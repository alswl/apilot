package workbench

import (
	"strings"
	"time"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/alswl/apilot/pkg/common"
	"github.com/egoist/mygo/ui"
)

type automationPreview struct {
	tasks    []common.Task
	selected string
	outcomes map[string][]string
}

func newAutomationPreview(tasks []common.Task, outcomes map[string][]string) automationPreview {
	now := time.Now()
	last := now.Add(-2 * time.Hour)
	for i := range tasks {
		task := &tasks[i]
		if task.Enabled {
			next := nextMorning(now)
			task.NextRunAt, task.Schedule.NextRunAt = &next, &next
		}
		if task.LastRun != nil {
			task.LastRun.FinishedAt = &last
		}
	}
	return automationPreview{tasks: tasks, outcomes: outcomes}
}

func (a *automationPreview) startRun(task *common.Task, after func(time.Duration, func())) {
	if task.LastRun != nil && task.LastRun.Active() {
		return
	}
	now := time.Now()
	run := &common.Run{TaskID: task.ID, Trigger: "manual", Status: "running", StartedAt: &now}
	task.LastRun = run
	a.selected = task.ID
	after(750*time.Millisecond, func() {
		finished := time.Now()
		run.FinishedAt = &finished
		run.Status = "succeeded"
		if outcomes := a.outcomes[task.ID]; len(outcomes) > 0 {
			run.Status = outcomes[0]
			a.outcomes[task.ID] = outcomes[1:]
		}
		if run.Status == "failed" {
			run.Error = "工作目录暂不可用，可重试"
		}
	})
}

func (a *automationPreview) view(c *ui.Context, after func(time.Duration, func())) {
	for i := range a.tasks {
		task := &a.tasks[i]
		ui.Row(c.Key(task.ID)).FillWidth().MinHeight(48).Padding(8).Gap(8).AlignItems(ui.Center).Children(func() {
			icon := "clock"
			if !task.Enabled {
				icon = "pause.circle"
			}
			plan := tr("每天 09:00")
			if task.Schedule.Type == "interval" {
				plan = tr("每 30 分钟")
			}
			if task.NextRunAt != nil {
				plan += tr(" · 下次 ") + task.NextRunAt.Format("01-02 15:04")
			} else if !task.Enabled {
				plan += tr(" · 已暂停")
			}
			if components.ListRow(c.Key("detail"), components.ListItem{
				Title: tr(task.Name), Subtitle: plan, Icon: icon, Status: scheduledResult(c, task.LastRun), Selected: a.selected == task.ID,
			}) {
				a.selected = task.ID
			}
			busy := task.LastRun != nil && task.LastRun.Active()
			label := tr("模拟运行 ") + task.Name
			if busy {
				label = tr(task.Name) + tr(" 正在运行")
			} else if task.LastRun != nil && task.LastRun.Trigger == "manual" && task.LastRun.Status == "failed" {
				label = tr("重试 ") + task.Name
			}
			if components.IconButton(c.Key("run"), "play", label, busy).Clicked() {
				a.startRun(task, after)
			}
		})
		ui.Divider(c.Key(task.ID + "-divider"))
		if a.selected == task.ID {
			components.Panel(c.Key(task.ID+"-detail"), func() { a.detail(c, task) })
		}
	}
}

func scheduledResult(c *ui.Context, run *common.Run) components.Status {
	if run == nil {
		return components.Status{Text: tr("尚未运行"), Color: c.Theme().TextMuted}
	}
	if run.Active() {
		return components.Status{Text: tr("正在运行"), Loading: true, Color: c.Theme().TextMuted}
	}
	if run.Status == "failed" {
		return components.Status{Text: tr("最近运行失败"), Icon: "exclamationmark.circle", Color: c.Theme().Danger}
	}
	return components.Status{Text: tr("最近运行成功"), Icon: "checkmark.circle", Color: c.Theme().Success}
}

func (a *automationPreview) detail(c *ui.Context, task *common.Task) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		components.Heading(c, tr("运行详情")).Grow(1)
		if components.IconButton(c.Key("close"), "xmark", tr("收起运行详情"), false).Clicked() {
			a.selected = ""
		}
	})
	components.Muted(c, tr("计划表达式 ")+task.Schedule.Expression)
	components.Muted(c, strings.Join(append([]string{task.Executable}, task.Args...), " "))
	if task.LastRun != nil && task.LastRun.FinishedAt != nil {
		components.Muted(c, tr("完成时间 ")+task.LastRun.FinishedAt.Format("01-02 15:04"))
	}
	if task.LastRun != nil && task.LastRun.Error != "" {
		components.DataText(c, tr(task.LastRun.Error))
	}
	ui.Checkbox(c.Key("automatic"), &task.Enabled, tr("自动执行"))
	if task.Schedule.Enabled != task.Enabled {
		task.Schedule.Enabled = task.Enabled
		task.NextRunAt = nil
		if task.Enabled {
			now := time.Now()
			next := nextMorning(now)
			if task.Schedule.Type == "interval" {
				next = now.Add(30 * time.Minute)
			}
			task.NextRunAt = &next
		}
		task.Schedule.NextRunAt = task.NextRunAt
	}
}

func nextMorning(now time.Time) time.Time {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	next := time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, local.Location())
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
