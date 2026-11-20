package integrations

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

var wbCheckbox = regexp.MustCompile(`^\s*(?:[-+*]|[0-9]+[.)])\s+\[([^\]\r\n])\]\s+(.+)$`)
var wbHeading = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)(?:[ \t]+#+)?[ \t\r]*$`)
var wbTag = regexp.MustCompile(`(?:^|\s)(#[\pL\pN_/-]+)`)
var wbField = regexp.MustCompile(`(?:^|\s)(🆔\s*[A-Za-z0-9_-]+|[📅⏳🛫✅➕❌]\s*\d{4}-\d{2}-\d{2}|[🔺⏫🔼🔽⏬]|⛔\s*[A-Za-z0-9_-]+(?:,\s*[A-Za-z0-9_-]+)*|🏁\s*(?:delete|keep)\b|🔁\s+[^📅⏳🛫✅➕❌🔺⏫🔼🔽⏬⛔🏁🆔#^\[\r\n]*[^📅⏳🛫✅➕❌🔺⏫🔼🔽⏬⛔🏁🆔#^\[\s])`)

var wbInlineField = regexp.MustCompile(`(?i)\[(created|start|scheduled|due|completion|cancelled|priority|repeat|onCompletion|id|dependsOn)::[ \t]*([^\]\r\n]+)\]|\((created|start|scheduled|due|completion|cancelled|priority|repeat|onCompletion|id|dependsOn)::[ \t]*([^()\r\n]+)\)`)
var wbTaskID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var wbTaskDependencies = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:,\s*[A-Za-z0-9_-]+)*$`)
var wbPriorities = map[rune]string{'🔺': "highest", '⏫': "high", '🔼': "medium", '🔽': "low", '⏬': "lowest"}
var wbDateKeys = map[rune]string{'📅': "due", '⏳': "scheduled", '🛫': "start", '✅': "done", '➕': "created", '❌': "cancelled"}

func scanWorkbenchMarkdown(content string, visit func(int, string)) {
	var fence byte
	var fenceLength int
	var comment, frontmatter bool
	for i, raw := range strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n") {
		line := strings.TrimSpace(raw)
		if i == 0 && line == "---" {
			frontmatter = true
			continue
		}
		if frontmatter {
			if line == "---" || line == "..." {
				frontmatter = false
			}
			continue
		}
		if !comment && len(line) >= 3 && (line[0] == '`' || line[0] == '~') {
			length := len(line) - len(strings.TrimLeft(line, string(line[0])))
			if length >= 3 && fence == 0 {
				fence, fenceLength = line[0], length
				continue
			}
			if fence == line[0] && length >= fenceLength && strings.TrimSpace(line[length:]) == "" {
				fence = 0
				continue
			}
		}
		if fence != 0 {
			continue
		}
		visit(i, workbenchWithoutComments(raw, &comment))
	}
}

func parseWorkbenchNote(c common.WorkbenchConnection, notePath, content string) []common.WorkbenchTask {
	var out []common.WorkbenchTask
	var sections []string
	var levels []int
	headingPath := ""
	scanWorkbenchMarkdown(content, func(i int, raw string) {
		if h := wbHeading.FindStringSubmatch(raw); h != nil {
			level := len(h[1])
			for len(levels) > 0 && levels[len(levels)-1] >= level {
				levels = levels[:len(levels)-1]
				sections = sections[:len(sections)-1]
			}
			levels = append(levels, level)
			sections = append(sections, h[2])
			encoded, _ := json.Marshal(sections)
			headingPath = string(encoded)
			return
		}
		m := wbCheckbox.FindStringSubmatch(raw)
		if m == nil {
			return
		}
		state := "unknown"
		switch strings.ToLower(m[1]) {
		case " ":
			state = "open"
		case "x":
			state = "done"
		default:
			if v := c.Statuses[m[1]]; v != "" {
				state = v
			} else if m[1] == "/" {
				state = "in_progress"
			} else if m[1] == "-" {
				state = "cancelled"
			}
		}
		fields := map[string]string{"path": notePath, "file": path.Base(notePath), "line": strconv.Itoa(i + 1), "section": "", "checkbox": m[1], "status": state}
		if len(sections) != 0 {
			fields["section"] = sections[len(sections)-1]
			fields["heading_path"] = headingPath
		}
		body := m[2]
		title := wbInlineField.ReplaceAllStringFunc(body, func(match string) string {
			groups := wbInlineField.FindStringSubmatch(match)
			key, value := groups[1], groups[2]
			if key == "" {
				key, value = groups[3], groups[4]
			}
			key = strings.ToLower(key)
			switch key {
			case "completion":
				key = "done"
			case "repeat":
				key = "recurrence"
			case "oncompletion":
				key = "on_completion"
			case "dependson":
				key = "depends_on"
			}
			if setWorkbenchTaskField(fields, key, strings.TrimSpace(value)) {
				return ""
			}
			return match
		})
		title = wbField.ReplaceAllStringFunc(title, func(match string) string {
			token := strings.TrimSpace(match)
			symbol := []rune(token)[0]
			value := strings.TrimSpace(strings.TrimPrefix(token, string(symbol)))
			if priority := wbPriorities[symbol]; priority != "" {
				fields["priority"] = priority
				return ""
			}
			key := wbDateKeys[symbol]
			switch symbol {
			case '🆔':
				key = "id"
			case '⛔':
				key = "depends_on"
			case '🔁':
				key = "recurrence"
			case '🏁':
				key = "on_completion"
			}
			if setWorkbenchTaskField(fields, key, value) {
				return ""
			}
			return match
		})
		id := fields["id"]
		title = strings.TrimSpace(title)
		if title == "" {
			title = body
		}
		var tags []string
		for _, tag := range wbTag.FindAllStringSubmatch(body, -1) {
			tags = append(tags, tag[1])
		}
		key := workbenchNoteKey(notePath, title)
		if id != "" {
			key = "task:" + id
		}
		out = append(out, common.WorkbenchTask{SourceKey: key, NativeID: id, Title: title, State: state, Locator: fmt.Sprintf("%s:%d", notePath, i+1), Labels: tags, Summary: body, NativeFields: fields})
	})
	return out
}

func setWorkbenchTaskField(fields map[string]string, key, value string) bool {
	switch key {
	case "created", "start", "scheduled", "due", "done", "cancelled":
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return false
		}
	case "id":
		if !wbTaskID.MatchString(value) {
			return false
		}
	case "depends_on":
		if !wbTaskDependencies.MatchString(value) {
			return false
		}
	case "priority":
		value = strings.ToLower(value)
		switch value {
		case "highest", "high", "medium", "normal", "low", "lowest", "none":
		default:
			return false
		}
	case "on_completion":
		value = strings.ToLower(value)
		if value != "delete" && value != "keep" {
			return false
		}
	case "recurrence":
		if value == "" {
			return false
		}
	default:
		return false
	}
	fields[key] = value
	return true
}

func workbenchWithoutComments(line string, comment *bool) string {
	var result strings.Builder
	for {
		if *comment {
			_, rest, ok := strings.Cut(line, "-->")
			if !ok {
				return result.String()
			}
			line = rest
			*comment = false
		}
		text, rest, ok := strings.Cut(line, "<!--")
		result.WriteString(text)
		if !ok {
			return result.String()
		}
		*comment = true
		line = rest
	}
}

func workbenchNoteKey(path, title string) string {
	return fmt.Sprintf("note:%d:%s%s", len(path), path, strings.ReplaceAll(title, "\x00", ""))
}
