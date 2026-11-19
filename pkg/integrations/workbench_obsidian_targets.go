package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/alswl/apilot/pkg/common"
)

func workbenchVisiblePath(path string) bool {
	if !filepath.IsLocal(path) || filepath.Clean(path) != path {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func openWorkbenchNoteRoot(c common.WorkbenchConnection) (*os.Root, error) {
	scope := filepath.Clean(c.Scope)
	if scope != "." && !workbenchVisiblePath(scope) {
		return nil, errVaultChanged
	}
	vault, err := os.OpenRoot(c.Vault)
	if err != nil {
		return nil, err
	}
	defer vault.Close()
	for part := scope; part != "."; part = filepath.Dir(part) {
		info, err := vault.Lstat(part)
		if err != nil || !info.IsDir() {
			return nil, errVaultChanged
		}
	}
	expected, err := vault.Lstat(scope)
	if err != nil {
		return nil, err
	}
	root, err := vault.OpenRoot(scope)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, info) {
		root.Close()
		return nil, errVaultChanged
	}
	return root, nil
}

func ListWorkbenchObsidianNotes(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchNote, error) {
	if c.ReadMethod == common.WorkbenchObsidianCLI {
		cli, err := openWorkbenchObsidianCLI(ctx, c)
		if err != nil {
			return nil, err
		}
		return cli.notes(ctx, c.Scope)
	}
	root, err := openWorkbenchNoteRoot(c)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	notes := []common.WorkbenchNote{}
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path != "." && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".md") {
			notes = append(notes, common.WorkbenchNote{NotePath: filepath.ToSlash(filepath.Join(c.Scope, path))})
		}
		return nil
	})
	return notes, err
}

func ReadWorkbenchObsidianNote(c common.WorkbenchConnection, notePath string) ([]byte, error) {
	if !workbenchVisiblePath(notePath) || !strings.EqualFold(filepath.Ext(notePath), ".md") {
		return nil, common.Err("INVALID_ARGUMENT", "Choose an in-scope Markdown note")
	}
	relative, err := filepath.Rel(filepath.Clean(c.Scope), notePath)
	if err != nil || !workbenchVisiblePath(relative) {
		return nil, common.Err("INVALID_ARGUMENT", "Note lies outside the connection scope")
	}
	root, err := openWorkbenchNoteRoot(c)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for part := filepath.Dir(relative); part != "."; part = filepath.Dir(part) {
		info, err := root.Lstat(part)
		if err != nil || !info.IsDir() {
			return nil, errVaultChanged
		}
	}
	expected, err := root.Lstat(relative)
	if err != nil {
		return nil, err
	}
	if !expected.Mode().IsRegular() {
		return nil, errVaultChanged
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	current, currentErr := root.Lstat(relative)
	if err != nil || currentErr != nil || len(data) > limit || !sameWorkbenchFile(expected, actual) || !sameWorkbenchFile(actual, current) {
		return nil, errVaultChanged
	}
	return data, nil
}

func WorkbenchObsidianHeadings(content []byte) []common.WorkbenchHeading {
	headings := []common.WorkbenchHeading{}
	var stack []common.WorkbenchHeading
	seen := map[string]int{}
	digest := sha256.Sum256(content)
	scanWorkbenchMarkdown(string(content), func(_ int, raw string) {
		matches := wbHeading.FindStringSubmatch(raw)
		if matches == nil || strings.TrimSpace(matches[2]) == "" {
			return
		}
		level := len(matches[1])
		for len(stack) > 0 && stack[len(stack)-1].Level >= level {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, parent := range stack {
			path = append(path, parent.HeadingPath[len(parent.HeadingPath)-1])
		}
		path = append(path, matches[2])
		key, _ := json.Marshal(path)
		seen[string(key)]++
		heading := common.WorkbenchHeading{HeadingPath: path, Level: level, TargetVersion: hex.EncodeToString(digest[:])}
		headings = append(headings, heading)
		stack = append(stack, heading)
	})
	for i := range headings {
		for n := range headings[i].HeadingPath {
			key, _ := json.Marshal(headings[i].HeadingPath[:n+1])
			if seen[string(key)] > 1 {
				headings[i].TargetVersion = ""
				headings[i].Reason = "Heading path is ambiguous"
				break
			}
		}
	}
	return headings
}

// InsertWorkbenchObsidianTask builds a conditional replacement from the
// current note, placing the task before the next header at any level.
func InsertWorkbenchObsidianTask(content []byte, path []string, version, title, marker string) ([]byte, error) {
	eligible := false
	for _, heading := range WorkbenchObsidianHeadings(content) {
		if slices.Equal(heading.HeadingPath, path) && heading.TargetVersion != "" && heading.TargetVersion == version {
			eligible = true
			break
		}
	}
	if !eligible {
		return nil, common.Err("STALE_REVISION", "The selected heading changed or is ambiguous")
	}
	if title == "" || strings.ContainsAny(title, "\r\n\x00") || strings.Contains(title, "🆔") ||
		strings.Contains(title, "<!--") || strings.Contains(title, "-->") {
		return nil, common.Err("INVALID_ARGUMENT", "Provide a single-line task title without an embedded task ID or comment")
	}
	var levels []int
	var segments []string
	start, boundary := -1, -1
	scanWorkbenchMarkdown(string(content), func(line int, raw string) {
		h := wbHeading.FindStringSubmatch(raw)
		if h == nil || strings.TrimSpace(h[2]) == "" || boundary >= 0 {
			return
		}
		if start >= 0 {
			boundary = line
			return
		}
		level := len(h[1])
		for len(levels) > 0 && levels[len(levels)-1] >= level {
			levels = levels[:len(levels)-1]
			segments = segments[:len(segments)-1]
		}
		levels = append(levels, level)
		segments = append(segments, h[2])
		if slices.Equal(segments, path) {
			start = line
		}
	})
	lines := strings.Split(string(content), "\n")
	if boundary < 0 {
		boundary = len(lines)
	}
	for boundary > start+1 && strings.TrimSpace(lines[boundary-1]) == "" {
		boundary--
	}
	newline := ""
	if strings.HasSuffix(lines[start], "\r") {
		newline = "\r"
	}
	line := "- [ ] " + title + " 🆔 " + marker + newline
	result := strings.Join(slices.Insert(lines, boundary, line), "\n")
	matches := 0
	for _, task := range parseWorkbenchNote(common.WorkbenchConnection{}, "", result) {
		if task.NativeID == marker {
			matches++
		}
	}
	if matches != 1 {
		return nil, common.Err("CONFLICT", "Inserted task is not uniquely visible in the note")
	}
	return []byte(result), nil
}

// SetWorkbenchObsidianCheckbox locates the previously observed task in the
// current note and changes only its checkbox byte.
func SetWorkbenchObsidianCheckbox(c common.WorkbenchConnection, content []byte, prior common.WorkbenchTask, state string) ([]byte, error) {
	path := prior.NativeFields["path"]
	line := -1
	for _, candidate := range parseWorkbenchNote(c, path, string(content)) {
		same := candidate.NativeID == prior.NativeID && candidate.Summary == prior.Summary
		if !same {
			continue
		}
		if line >= 0 || candidate.NativeFields["checkbox"] != prior.NativeFields["checkbox"] {
			return nil, common.Err("STALE_REVISION", "Task changed or is not unique")
		}
		number, err := strconv.Atoi(candidate.NativeFields["line"])
		if err != nil {
			return nil, common.Err("CONFLICT", "Task position is unavailable")
		}
		line = number - 1
	}
	if line < 0 {
		return nil, common.Err("STALE_REVISION", "Task could not be located in the note")
	}
	lines := strings.Split(string(content), "\n")
	raw := lines[line]
	prefix := ""
	if line == 0 && strings.HasPrefix(raw, "\ufeff") {
		prefix = "\ufeff"
		raw = strings.TrimPrefix(raw, prefix)
	}
	match := wbCheckbox.FindStringSubmatchIndex(raw)
	if len(match) < 6 || raw[match[2]:match[3]] != prior.NativeFields["checkbox"] {
		return nil, common.Err("CONFLICT", "Raw checkbox location could not be verified")
	}
	checkbox := " "
	if state == "done" {
		checkbox = "x"
	}
	lines[line] = prefix + raw[:match[2]] + checkbox + raw[match[3]:]
	return []byte(strings.Join(lines, "\n")), nil
}
