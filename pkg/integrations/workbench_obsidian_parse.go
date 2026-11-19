package integrations

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

var wbCheckbox = regexp.MustCompile(`^\s*(?:[-+*]|[0-9]+[.)])\s+\[([^\]\r\n])\]\s+(.+)$`)
var wbHeading = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
var wbTag = regexp.MustCompile(`(?:^|\s)(#[\pL\pN_/-]+)`)
var wbField = regexp.MustCompile(`(?:^|\s)(🆔\s*[A-Za-z0-9_-]+|[📅⏳🛫✅]\s*\d{4}-\d{2}-\d{2}|[🔺⏫🔼🔽⏬])`)

func parseWorkbenchNote(c common.WorkbenchConnection, path, content string) []common.WorkbenchTask {
	var out []common.WorkbenchTask
	var section string
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
		raw = workbenchWithoutComments(raw, &comment)
		if h := wbHeading.FindStringSubmatch(raw); h != nil {
			section = h[1]
			continue
		}
		m := wbCheckbox.FindStringSubmatch(raw)
		if m == nil {
			continue
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
			}
		}
		fields := map[string]string{"path": path, "line": strconv.Itoa(i + 1), "section": section, "checkbox": m[1], "status": state}
		body := m[2]
		var id string
		title := wbField.ReplaceAllStringFunc(body, func(match string) string {
			token := strings.TrimSpace(match)
			symbol := []rune(token)[0]
			value := strings.TrimSpace(strings.TrimPrefix(token, string(symbol)))
			if symbol == '🆔' {
				id = value
				return ""
			}
			dateKey := map[rune]string{'📅': "due", '⏳': "scheduled", '🛫': "start", '✅': "done"}[symbol]
			if dateKey != "" {
				if _, err := time.Parse("2006-01-02", value); err != nil {
					return match
				}
				fields[dateKey] = value
			} else {
				fields["priority"] = map[rune]string{'🔺': "highest", '⏫': "high", '🔼': "medium", '🔽': "low", '⏬': "lowest"}[symbol]
			}
			return ""
		})
		title = strings.TrimSpace(title)
		if title == "" {
			title = body
		}
		var tags []string
		for _, tag := range wbTag.FindAllStringSubmatch(body, -1) {
			tags = append(tags, tag[1])
		}
		key := workbenchNoteKey(path, title)
		if id != "" {
			key = "task:" + id
		}
		out = append(out, common.WorkbenchTask{SourceKey: key, NativeID: id, Title: title, State: state, Locator: fmt.Sprintf("%s:%d", path, i+1), Labels: tags, Summary: body, NativeFields: fields})
	}
	return out
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
