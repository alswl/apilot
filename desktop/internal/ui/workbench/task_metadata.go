package workbench

import (
	"slices"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) workbenchLabelProperty(c *ui.Context, object *sourceObject) {
	detail := m.taskDetailUI(object.ID)
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		components.Muted(c, tr("工作台标签"))
		if len(object.WorkbenchLabels) == 0 {
			components.Muted(c.Key("empty"), tr("无标签"))
		}
		ui.Row(c.Key("labels")).FillWidth().Wrap().Gap(6).Children(func() {
			for _, label := range object.WorkbenchLabels {
				ui.Row(c.Key(label.ID)).Gap(2).AlignItems(ui.Center).Children(func() {
					components.Tag(c, label.Name)
					if components.IconButton(c.Key("remove"), "xmark", tr("移除工作台标签 ")+label.Name, detail.localLabelSaving).Clicked() {
						names := []string{}
						for _, current := range object.WorkbenchLabels {
							if current.ID != label.ID {
								names = append(names, current.Name)
							}
						}
						m.saveTaskLabels(object.ID, detail, names, false)
					}
				})
			}
		})
		ui.Row(c.Key("add")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			input := ui.TextInput(c.Key("input"), &detail.localLabelDraft).Label(tr("添加工作台标签")).Placeholder(tr("添加标签")).Grow(1).MinWidth(0).Disabled(detail.localLabelSaving)
			m.keyboard.editingText = m.keyboard.editingText || input.Focused()
			name := strings.TrimSpace(detail.localLabelDraft)
			button := components.IconButton(c.Key("save"), "plus", tr("添加工作台标签"), name == "" || detail.localLabelSaving)
			if (input.Submitted() || button.Clicked()) && name != "" && !detail.localLabelSaving {
				names := make([]string, 0, len(object.WorkbenchLabels)+1)
				for _, label := range object.WorkbenchLabels {
					names = append(names, label.Name)
				}
				if !slices.Contains(names, name) {
					names = append(names, name)
				}
				m.saveTaskLabels(object.ID, detail, names, true)
			}
		})
		if detail.localLabelProblem != "" {
			components.Muted(c.Key("error"), detail.localLabelProblem)
		}
	})
}

func (m *mockup) saveTaskLabels(taskID string, detail *taskDetailUI, names []string, clearDraft bool) {
	if detail.localLabelSaving {
		return
	}
	detail.localLabelSaving, detail.localLabelProblem = true, ""
	remote := m.live
	go func() {
		err := remote.SetWorkbenchLabels(taskID, names)
		var updated common.WorkbenchTask
		if err == nil {
			updated, err = remote.WorkbenchTask(taskID)
		}
		mygo.RunOnMain(func() {
			if m.taskDetails[taskID] != detail {
				return
			}
			detail.localLabelSaving = false
			if err != nil {
				detail.localLabelProblem = common.ProtocolError(err).Message
			} else {
				for i := range m.objects {
					if m.objects[i].ID == taskID {
						m.objects[i].WorkbenchLabels = updated.WorkbenchLabels
						break
					}
				}
				if clearDraft {
					detail.localLabelDraft = ""
				}
			}
			m.liveGeneration++
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
				m.refreshLive()
			}
		})
	}()
}

func (m *mockup) loadTaskAnnotations(taskID string, detail *taskDetailUI) {
	if detail.annotationsLoading {
		return
	}
	detail.annotationsLoading = true
	detail.annotationGeneration++
	generation := detail.annotationGeneration
	remote := m.live
	go func() {
		items, err := remote.WorkbenchAnnotations(taskID)
		mygo.RunOnMain(func() {
			if m.taskDetails[taskID] != detail || detail.annotationGeneration != generation {
				return
			}
			detail.annotationsLoading, detail.annotationsLoaded = false, true
			if err != nil {
				problem := common.ProtocolError(err)
				detail.annotationProblem = problem.Message
				if problem.Code == "NOT_FOUND" {
					detail.annotations = nil
				}
			} else {
				detail.annotations, detail.annotationProblem = items, ""
			}
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
			}
		})
	}()
}

func (m *mockup) deleteTaskAnnotation(taskID string, detail *taskDetailUI, annotationID string) {
	if detail.annotationSaving {
		return
	}
	detail.annotationSaving, detail.annotationProblem = true, ""
	remote := m.live
	go func() {
		err := remote.DeleteWorkbenchAnnotation(taskID, annotationID)
		mygo.RunOnMain(func() {
			if m.taskDetails[taskID] != detail {
				return
			}
			detail.annotationSaving = false
			if err != nil {
				detail.annotationProblem = common.ProtocolError(err).Message
			} else {
				detail.annotationGeneration++
				detail.annotationsLoading, detail.annotationsLoaded = false, false
				m.loadTaskAnnotations(taskID, detail)
			}
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
			}
		})
	}()
}

func (m *mockup) saveTaskAnnotation(taskID string, detail *taskDetailUI, body string) {
	if detail.annotationSaving {
		return
	}
	if detail.annotationID == "" || detail.annotationBody != body {
		detail.annotationID, detail.annotationBody = common.ID(), body
	}
	id := detail.annotationID
	detail.annotationSaving, detail.annotationProblem = true, ""
	remote := m.live
	go func() {
		err := remote.PutWorkbenchAnnotation(taskID, id, body)
		mygo.RunOnMain(func() {
			if m.taskDetails[taskID] != detail {
				return
			}
			detail.annotationSaving = false
			if err != nil {
				detail.annotationProblem = common.ProtocolError(err).Message
			} else {
				if detail.draft == body {
					detail.draft = ""
				}
				detail.annotationID, detail.annotationBody = "", ""
				detail.annotationGeneration++
				detail.annotationsLoading, detail.annotationsLoaded = false, false
				m.loadTaskAnnotations(taskID, detail)
			}
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
			}
		})
	}()
}
