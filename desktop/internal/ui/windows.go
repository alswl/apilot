package ui

import "github.com/egoist/mygo"

// Keep one draft attached to the management window, including tray entry points.
func (s *windowState) focusEditor() bool {
	if s.confirming {
		if s.win != nil && !s.win.IsDestroyed() {
			s.win.Focus()
		}
		return true
	}
	var win *mygo.Window
	if s.taskEditor != nil {
		win = s.taskEditor.win
	} else {
		win = s.observerEditor
	}
	if win == nil || win.IsDestroyed() {
		return false
	}
	win.Show()
	win.Focus()
	return true
}

func (s *windowState) confirmDelete(message, detail, button string, remove func() error) {
	if s.stopping || s.busy || s.focusEditor() {
		return
	}
	s.confirming = true
	parent := s.win
	options := mygo.MessageOptions{Parent: parent, Type: mygo.MessageWarning, Message: message, Detail: detail, Buttons: []string{tr("cancel"), button}, CancelButton: 0, DefaultButton: 0, DestructiveButtons: []int{1}}
	// Native dialogs are dismissed by application quit; do not include their wait
	// in the workers that must drain before quit can close windows.
	go func() {
		result, err := mygo.Dialog.Message(options)
		s.update(func() {
			s.confirming = false
			if parent.IsDestroyed() {
				return
			}
			if err != nil {
				s.err = err.Error()
				return
			}
			if result.Button == 1 {
				s.action(remove)
			}
		})
	}()
}
