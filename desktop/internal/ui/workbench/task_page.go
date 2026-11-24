package workbench

import (
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) openCurrentTask() {
	if index := m.currentTaskIndex(); index >= 0 {
		m.openFullTask(index)
		return
	}
	if page := m.pagedTaskRows(m.taskRows()); len(page) > 0 {
		m.openFullTask(page[0])
	}
}

func (m *mockup) openFullTask(index int) {
	if m.currentTaskIndex() != index {
		m.taskBackStack = nil
	}
	m.showTaskDetail(index)
}

func (m *mockup) showTaskDetail(index int) {
	previous := m.taskSelection
	m.openTask(index)
	m.fullTask = true
	if !m.locateTaskPage(m.taskRows(), index) {
		m.taskSelection = previous
	}
	if m.unifiedTasks() {
		m.layout.detailReturnHidden = m.layout.detailReturnHidden || m.layout.detailHidden
		m.layout.detailHidden = false
		m.restoreTaskFocus = false
		m.layout.focusDetail = true
		m.layout.revealSelection = true
	}
}

func (m *mockup) taskPreview(c *ui.Context) {
	i := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == m.taskSelection })
	if i < 0 {
		return
	}
	o := m.objects[i]
	components.Inspector(c, components.InspectorOptions{
		Title: tr("快速预览"), DetailID: o.ID, Width: 300, CloseLabel: tr("收起任务详情"),
		HeaderActions: func(c *ui.Context) {
			if components.IconButton(c.Key("open"), "arrow.up.right", tr("打开完整任务"), false).
				Tooltip(tr("打开完整任务")+" · Return").KeyShortcuts(0, ui.KeyEnter).Clicked() {
				m.openFullTask(i)
			}
		},
	}, func() { m.taskSelection = ""; m.restoreTaskFocus = true }, func() {
		components.Title(c, m.sourceText(o.Title))
		components.Muted(c, displayTaskID(o)+" · "+tr(m.connectionName(o.ConnectionID)))
		components.PropertyRow(c.Key("state"), tr("状态"), m.taskStateLabel(o))
		if due := o.NativeFields["due"]; due != "" {
			components.PropertyRow(c.Key("due"), tr("截止日"), tr(due))
		}
		if len(o.Activity) > 0 {
			event := o.Activity[len(o.Activity)-1]
			components.ContentSection(c.Key("latest"), tr("最近进展"), func() {
				components.ActivityRow(c, taskActivityTime(event), tr(event.Text))
			})
		}
	})
}

func (m *mockup) taskPage(c *ui.Context) {
	o := &m.objects[m.selected]
	width, _ := c.Size()
	ui.Column(c).Grow(1).FillHeight().MinWidth(0).Background(c.Theme().Surface).Padding(16).Gap(12).Children(func() {
		m.taskBackLink(c.Key("return-to-source"))
		ui.Row(c.Key("header")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if components.IconButton(c.Key("back"), "chevron.left", tr("返回")+tr("任务"), false).KeyShortcuts(0, ui.KeyEscape).Clicked() {
				m.closeInspector()
			}
			ui.Text(c, tr("任务")+" / "+displayTaskID(*o)).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
			ui.Spacer(c).Grow(1)
			m.taskWatchButton(c.Key("watch"), o)
			m.taskSourceControls(c.Key("source-actions"), o)
		})
		m.taskSourceFeedback(c.Key("write-feedback"), o)
		ui.Divider(c)
		if width < 1080 {
			ui.Scroll(c.Key(o.ID)).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(16).Children(func() {
					m.taskPageTitle(c.Key("overview"), o)
					m.taskPageProperties(c.Key("properties"), o)
					m.taskDescription(c.Key("description"), o)
					m.taskPageBody(c.Key("body"), o)
				})
			})
			return
		}
		ui.Row(c.Key("detail-columns")).Grow(1).FillWidth().Gap(16).Children(func() {
			ui.Scroll(c.Key(o.ID + "-body")).Grow(1).MinWidth(0).FillHeight().Children(func() {
				ui.Column(c).FillWidth().MaxWidth(680).Padding(0, 16).Gap(20).Children(func() {
					m.taskPageTitle(c.Key("overview"), o)
					m.taskDescription(c.Key("description"), o)
					m.taskPageBody(c.Key("body"), o)
				})
			})
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border)
			ui.Scroll(c.Key(o.ID + "-properties")).Width(268).Shrink(0).FillHeight().Children(func() { m.taskPageProperties(c, o) })
		})
	})
}

func (m *mockup) taskPageTitle(c *ui.Context, o *sourceObject) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		components.DocumentTitle(c.Key("title"), m.sourceText(o.Title))
		if o.Mode == "mock" {
			components.Muted(c.Key("source-mode"), tr("模拟数据"))
		}
		ui.Column(c.Key("overview")).FillWidth().MaxWidth(360).Children(func() {
			components.PropertyGrid(c, []components.Property{
				{ID: "status", Title: tr("状态"), Value: m.taskStateLabel(*o)},
				{ID: "source", Title: tr("来源"), Value: tr(m.connectionName(o.ConnectionID))},
			}, 2)
		})
	})
}

func (m *mockup) taskDescription(c *ui.Context, o *sourceObject) {
	if m.live != nil && m.isManagedConnector(o.Kind) {
		m.managedTaskDetail(c, o, m.taskDetailUI(o.ID))
		return
	}
	if o.Summary != "" {
		components.Body(c, m.sourceText(o.Summary))
	}
}

func (m *mockup) taskPageBody(c *ui.Context, o *sourceObject) {
	ui.Column(c).FillWidth().Gap(16).Children(func() {
		if excerpt := o.NativeFields["excerpt"]; excerpt != "" {
			components.ContentSection(c.Key("source"), tr("来源摘录"), func() {
				components.Muted(c, m.sourceText(o.Locator))
				components.Body(c, tr(excerpt))
			})
		}
		m.taskLinks(c.Key("links"), o)
		if len(o.Activity) > 0 {
			components.ContentSection(c.Key("events"), tr("活动"), func() { m.taskActivity(c, o) })
		}
	})
}

func (m *mockup) taskLinks(c *ui.Context, o *sourceObject) {
	if len(o.Related) > 0 {
		components.ContentSection(c.Key("related"), tr("相关资料与产出"), func() {
			for i, ref := range o.Related {
				ui.Row(c.Key(i)).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					components.Muted(c, tr(ref.Kind))
					if components.TextAction(c.Key("open"), tr(ref.Title)) {
						index := slices.IndexFunc(m.objects, func(item sourceObject) bool { return item.ID == ref.TaskID })
						if index >= 0 {
							m.followRelatedTask(index)
						} else {
							m.taskResource, m.taskResourceOpen = ref, true
						}
					}
				})
			}
		})
	}
}

func obsidianNoteURL(vault, note string) string {
	localPath := filepath.FromSlash(note)
	if !filepath.IsAbs(vault) || !filepath.IsLocal(localPath) || filepath.Clean(localPath) != localPath ||
		!strings.EqualFold(filepath.Ext(localPath), ".md") {
		return ""
	}
	query := url.QueryEscape(filepath.Join(vault, localPath))
	return "obsidian://open?path=" + strings.ReplaceAll(query, "+", "%20")
}

func (m *mockup) obsidianSourceLink(c *ui.Context, o *sourceObject) {
	conn := m.taskConnection(*o)
	if conn == nil {
		return
	}
	link, label, failure := "", "", ""
	switch conn.SourceKind {
	case common.WorkbenchObsidianTasks:
		link, label, failure = obsidianNoteURL(conn.VaultPath, o.NativeFields["path"]), "在 Obsidian 中打开", "无法打开 Obsidian"
	case common.WorkbenchGitHubRepository:
		link, label, failure = githubIssueURL(conn.Repository, o.NativeID), "在 GitHub 中打开", "无法打开 GitHub"
	}
	if link != "" {
		detail := m.taskDetailUI(o.ID)
		if components.TextAction(c.Key("open"), tr(label)) {
			detail.sourceOpenFailed = mygo.Shell.OpenExternal(link) != nil
		}
		if detail.sourceOpenFailed {
			components.Muted(c.Key("error"), tr(failure))
		}
	}
}

func githubIssueURL(repository, number string) string {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "?#%\\ \t\r\n") {
			return ""
		}
	}
	id, err := strconv.ParseUint(number, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != number {
		return ""
	}
	return "https://github.com/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues/" + number
}

func taskActivityTime(event taskEvent) string {
	if event.At != "" {
		return tr(event.At)
	}
	if !event.Time.IsZero() {
		return event.Time.Local().Format("2006-01-02 15:04:05")
	}
	return ""
}

func (m *mockup) taskActivity(c *ui.Context, o *sourceObject) {
	for i := len(o.Activity) - 1; i >= 0; i-- {
		event := o.Activity[i]
		components.ActivityRow(c.Key(i), taskActivityTime(event), tr(event.Text))
	}
}

func (m *mockup) taskPageProperties(c *ui.Context, o *sourceObject) {
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		components.Heading(c, tr("属性"))
		m.taskProperties(c, o)
	})
}

func (m *mockup) taskWatchButton(c *ui.Context, o *sourceObject) {
	watched := m.isWatched(o.ID)
	icon, label := "bookmark", "关注任务"
	if o.ID == m.mutation.SourceObject {
		label = "关注验收变化"
	}
	if watched {
		icon, label = "bookmark.fill", "取消关注任务"
	}
	button := components.IconToggle(c, icon, tr(label), watched)
	if shortcut := m.keyboard.display("task.watch"); shortcut != "" {
		button.Tooltip(tr(label) + " · " + shortcut)
	}
	if button.Clicked() {
		m.setTaskWatched(o.ID, !watched)
	}
}

func (m *mockup) resourceDialog(c *ui.Context) {
	ui.Modal(c, &m.taskResourceOpen, func() {
		ui.Column(c).Width(460).Gap(12).Children(func() {
			components.Title(c, tr(m.taskResource.Title))
			components.Muted(c, tr(m.taskResource.Kind))
			components.Body(c, tr(m.taskResource.Body))
		})
	})
}

func (m *mockup) recordTaskEvent(id, description string) {
	for i := range m.objects {
		if m.objects[i].ID == id {
			m.objects[i].Activity = append(m.objects[i].Activity, taskEvent{At: "刚刚", Time: time.Now(), Text: description})
			return
		}
	}
}

func taskUpdated(object sourceObject) time.Time {
	if len(object.Activity) == 0 {
		return time.Time{}
	}
	return object.Activity[len(object.Activity)-1].Time
}

func (m *mockup) isWatched(id string) bool {
	if id == m.mutation.SourceObject {
		return m.watching
	}
	return m.taskWatches[id]
}

func (m *mockup) setTaskWatched(id string, watching bool) {
	if id == m.mutation.SourceObject {
		m.setWatching(watching)
		return
	}
	m.taskWatches[id] = watching
	if watching {
		m.recordTaskEvent(id, "开始关注任务")
	} else {
		m.recordTaskEvent(id, "取消关注任务")
	}
}
