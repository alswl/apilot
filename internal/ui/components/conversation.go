package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func AssistantMessage(c *ui.Context, author, timestamp string, content func()) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		if author != "" || timestamp != "" {
			ui.Row(c.Key("metadata")).Gap(8).Children(func() {
				if author != "" {
					Muted(c, author)
				}
				if timestamp != "" {
					Muted(c, timestamp)
				}
			})
		}
		content()
	})
}

func TextAction(c *ui.Context, label string) bool {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).MinHeight(28).Padding(4, 0).Justify(ui.Start)
	button.Children(func() { DataText(c, label).Underline() })
	return button.Clicked()
}

func UserMessage(c *ui.Context, text string) {
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		ui.Column(c.Key("bubble")).MaxWidth(460).Padding(12).Radius(8).
			Background(c.Theme().Selection).Children(func() { Body(c, text) })
	})
}

func ConversationTurn(c *ui.Context, question, reply string, references func()) {
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		UserMessage(c.Key("question"), question)
		AssistantMessage(c.Key("reply"), "", "", func() {
			Body(c, reply)
			if references != nil {
				references()
			}
		})
	})
}

type ComposerLabels struct {
	Focus                    bool
	Input, Placeholder, Send string
}

// Composer returns a trimmed user submission. The caller owns the draft
// and determines when to clear it and whether to send the message.
func Composer(c *ui.Context, draft *string, labels ComposerLabels) string {
	var submission string
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		input := ui.TextInput(c.Key("input"), draft).Label(labels.Input).
			Placeholder(labels.Placeholder).Grow(1).MinWidth(0)
		if labels.Focus {
			input.Focus()
		}
		value := strings.TrimSpace(*draft)
		send := ui.ButtonBase(c.Key("send")).Label(labels.Send).Tooltip(labels.Send).
			Size(32, 32).Radius(6).Center().Disabled(value == "").Background(c.Theme().Accent)
		send.Children(func() { Symbol(c, "arrow.up", c.Theme().AccentText) })
		if send.Clicked() || input.Submitted() {
			submission = value
		}
	})
	return submission
}

// ChatReply gives assistant messages a stable identity and a separate body.
func ChatReply(c *ui.Context, author, timestamp string, content func()) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Start).Children(func() {
		ui.Box(c.Key("avatar")).Size(30, 30).Shrink(0).Radius(8).Background(c.Theme().SurfaceHover).Center().Children(func() {
			Symbol(c, "sparkles", c.Theme().Accent)
		})
		ui.Column(c.Key("message")).Grow(1).MinWidth(0).Gap(8).Children(func() {
			AssistantMessage(c, author, timestamp, content)
		})
	})
}

func ChatTurn(c *ui.Context, question, reply string, references func()) {
	ui.Column(c).FillWidth().Gap(16).Children(func() {
		UserMessage(c.Key("question"), question)
		ChatReply(c.Key("reply"), "Apilot", "", func() {
			Body(c, reply)
			if references != nil {
				references()
			}
		})
	})
}
