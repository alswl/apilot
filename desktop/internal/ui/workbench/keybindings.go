package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/config"
	"github.com/egoist/mygo/ui"
)

type keystroke struct {
	key  ui.Key
	mods ui.Modifiers
}

type binding struct {
	command, label string
	first, second  keystroke
	plain          bool
}

type keyboardState struct {
	path, loadError          string
	bindings                 []binding
	prefix                   keystroke
	expires                  time.Time
	palette, help, vim       bool
	focusNavigation          bool
	query                    string
	cursor                   int
	focusSearch, editingText bool
}

var defaultBindings = map[string][]string{
	"view.prevPage": {}, "view.nextPage": {},
	"task.edit": {"a"}, "task.labels": {"l"}, "task.toggle": {"x"},
	"command.palette": {"Cmd+K"},
	"navigation.now":  {"Cmd+1", "g n"}, "navigation.tasks": {"Cmd+2", "g t"},
	"navigation.connections": {"Cmd+3", "g c"}, "navigation.scheduled": {"Cmd+4", "g s"}, "navigation.usage": {"Cmd+5", "Cmd+9"},
	"tasks.next": {"j"}, "tasks.previous": {"k"}, "tasks.search": {"Cmd+F", "/"},
	"shortcuts.help": {"Cmd+/", "?"},
	"tasks.first":    {}, "tasks.last": {}, "tasks.insert": {},
	"task.new": {"Cmd+N", "n"}, "task.evidence": {"e"}, "task.watch": {"w"}, "task.editDeadline": {"d"},
	"now.reply": {"r"}, "view.save": {"Alt+V"}, "task.open": {"o"}, "settings.open": {},
}

func parseStroke(value string) (keystroke, error) {
	parts := strings.Split(strings.ToLower(value), "+")
	key := parts[len(parts)-1]
	if key == "?" && len(parts) == 1 {
		return keystroke{key: ui.KeySlash, mods: ui.Shift}, nil
	}
	var stroke keystroke
	for _, part := range parts[:len(parts)-1] {
		modifiers := map[string]ui.Modifiers{"cmd": ui.Cmd, "shift": ui.Shift, "alt": ui.Alt, "ctrl": ui.Ctrl}
		mod := modifiers[part]
		if mod == 0 || stroke.mods&mod != 0 {
			return keystroke{}, fmt.Errorf("invalid modifiers in %q", value)
		}
		stroke.mods |= mod
	}
	switch {
	case len(key) == 1 && key[0] >= 'a' && key[0] <= 'z':
		stroke.key = ui.KeyA + ui.Key(key[0]-'a')
	case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
		stroke.key = ui.Key0 + ui.Key(key[0]-'0')
	default:
		stroke.key = map[string]ui.Key{
			"/": ui.KeySlash, ",": ui.KeyComma, ".": ui.KeyPeriod, "-": ui.KeyMinus,
			"=": ui.KeyEqual, ";": ui.KeySemicolon, "[": ui.KeyBracketLeft, "]": ui.KeyBracketRight,
		}[key]
	}
	if stroke.key == ui.KeyUnknown {
		return keystroke{}, fmt.Errorf("unsupported key %q", value)
	}
	if stroke.mods&ui.Cmd != 0 && strings.Contains("acqsvwxz,hm", key) {
		return keystroke{}, fmt.Errorf("reserved macOS shortcut %q", value)
	}
	return stroke, nil
}

func compileBindings(settings config.Keybindings) ([]binding, error) {
	effective := maps.Clone(defaultBindings)
	switch settings.Mode {
	case "", "standard":
	case "vim":
		effective["tasks.first"] = []string{"g g"}
		effective["tasks.last"] = []string{"Shift+G"}
		effective["tasks.insert"] = []string{"i"}
		settings.EnableSingleKeys = true
	default:
		return nil, fmt.Errorf("unknown keyboard mode %q", settings.Mode)
	}
	for id, keys := range settings.Bindings {
		if _, ok := effective[id]; !ok {
			return nil, fmt.Errorf("unknown shortcut command %q", id)
		}
		effective[id] = keys
	}
	var result []binding
	for _, id := range slices.Sorted(maps.Keys(effective)) {
		for _, label := range effective[id] {
			keys := strings.Fields(label)
			if len(keys) < 1 || len(keys) > 2 {
				return nil, fmt.Errorf("%s: invalid binding %q", id, label)
			}
			item := binding{command: id, label: label}
			var err error
			item.first, err = parseStroke(keys[0])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", id, err)
			}
			item.plain = item.first.mods&(ui.Cmd|ui.Ctrl|ui.Alt) == 0
			if len(keys) == 2 {
				item.second, err = parseStroke(keys[1])
				if err != nil || !item.plain || item.second.mods != 0 {
					return nil, fmt.Errorf("%s: invalid two-key sequence %q", id, label)
				}
			}
			for _, other := range result {
				if other.first == item.first && (other.second == item.second || other.second.key == 0 || item.second.key == 0) {
					return nil, fmt.Errorf("%q (%s) conflicts with %q (%s)", label, id, other.label, other.command)
				}
			}
			result = append(result, item)
		}
	}
	if !settings.EnableSingleKeys {
		result = slices.DeleteFunc(result, func(item binding) bool { return item.plain })
	}
	return result, nil
}

func defaultKeyboard() keyboardState {
	bindings, err := compileBindings(config.Keybindings{Mode: config.DefaultKeyboardMode})
	if err != nil {
		panic(err)
	}
	return keyboardState{bindings: bindings, vim: config.DefaultKeyboardMode == "vim"}
}

func (k *keyboardState) reload() {
	settings, err := config.ReadKeybindings(k.path)
	if err == nil {
		var bindings []binding
		bindings, err = compileBindings(settings)
		if err == nil {
			k.bindings, k.prefix, k.loadError = bindings, keystroke{}, ""
			k.vim = settings.Mode == "vim"
			return
		}
	}
	k.loadError = err.Error()
}

func (k *keyboardState) display(id string) string {
	var labels []string
	for _, item := range k.bindings {
		if item.command == id {
			labels = append(labels, item.label)
		}
	}
	return strings.Join(labels, "  /  ")
}

func (k *keyboardState) observe(event ui.InputEvent) {
	if k.prefix.key == 0 {
		return
	}
	switch event.Kind {
	case ui.InputPointerDown:
		k.prefix = keystroke{}
	case ui.InputKeyDown:
		for _, item := range k.bindings {
			if item.first == k.prefix && item.second.key == event.Key && item.second.mods == event.Mods {
				return
			}
		}
		k.prefix = keystroke{}
	}
}
