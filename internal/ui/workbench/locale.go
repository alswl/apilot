package workbench

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

//go:embed fixtures/en.json
var englishJSON []byte

var english = func() map[string]string {
	var messages map[string]string
	if err := json.Unmarshal(englishJSON, &messages); err != nil {
		panic(err)
	}
	return messages
}()

var englishUI atomic.Bool

func resolveLanguage(language, system string) (bool, error) {
	switch language {
	case "en":
		return true, nil
	case "zh-Hans":
		return false, nil
	case "", "system":
		return !strings.HasPrefix(strings.ToLower(system), "zh"), nil
	default:
		return false, fmt.Errorf("unknown language %q (use en, zh-Hans, or system)", language)
	}
}

func tr(message string) string {
	if englishUI.Load() {
		if value, ok := english[message]; ok {
			return value
		}
	}
	return message
}

func localizedSelect(c *ui.Context, value *string, options []string, width float32) {
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = tr(option)
	}
	selected := tr(*value)
	element := ui.Select(c, &selected, labels)
	if width == 0 {
		element.FillWidth()
	} else {
		element.Width(width)
	}
	if element.Changed() {
		if i := slices.Index(labels, selected); i >= 0 {
			*value = options[i]
		}
	}
}
