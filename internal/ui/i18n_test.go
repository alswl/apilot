package ui

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCatalogCoverage(t *testing.T) {
	read := func(locale string) map[string]string {
		data, err := os.ReadFile("locales/" + locale + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		return messages
	}
	english, chinese := read("en"), read("zh-Hans")
	for key, text := range english {
		if text == "" || chinese[key] == "" {
			t.Errorf("missing translation: %q", key)
		}
		// Check printf argument contracts across translations.
		if got, want := strings.Count(text, "%"), strings.Count(chinese[key], "%"); got != want {
			t.Errorf("placeholder mismatch: %q", key)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "tr" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				t.Errorf("nonliteral translation in %s", name)
				return true
			}
			key, _ := strconv.Unquote(literal.Value)
			if english[key] == "" {
				t.Errorf("missing key %q", key)
			}
			return true
		})
	}
}

func TestLocalizedUI(t *testing.T) {
	defer displayLocale.Store("en")
	for _, locale := range []string{"en", "zh-Hans"} {
		t.Run(locale, func(t *testing.T) {
			s := &windowState{}
			tt := ui.NewTester(s.view, 480, 360)
			displayLocale.Store(locale)
			tt.SetLocale(locale)
			if !tt.HasText(translate(locale, "noTasks")) {
				t.Fatal(tt.Texts())
			}
			s.tab = 1
			tt = ui.NewTester(s.view, 480, 360)
			displayLocale.Store(locale)
			tt.SetLocale(locale)
			if !tt.HasText(translate(locale, "noRunsYet")) {
				t.Fatal(tt.Texts())
			}
		})
	}
	if got := translate("fr", "save"); got != "Save" {
		t.Fatal(got)
	}
	if got := fmt.Sprintf(translate("en", "deleteTaskS"), "中文 user task"); got != "Delete task “中文 user task”?" {
		t.Fatal(got)
	}

}

func TestLanguagePicker(t *testing.T) {
	defer displayLocale.Store("en")
	for _, locale := range []string{"en", "zh-Hans"} {
		t.Run(locale, func(t *testing.T) {
			displayLocale.Store(locale)
			selected, applied := "", "unchanged"
			tt := ui.NewTester(func(c *ui.Context) {
				languagePicker(c, &selected, func(tag string) { applied = tag })
			}, 480, 160)
			if !tt.HasText(translate(locale, "language")) {
				t.Fatal(tt.Texts())
			}
			for _, option := range []struct{ label, tag string }{{"简体中文", "zh-Hans"}, {"English", "en"}, {translate(locale, "systemLanguage"), ""}} {
				if err := tt.Click(option.label); err != nil {
					t.Fatal(err)
				}
				if selected != option.tag || applied != option.tag {
					t.Fatalf("%s: selected=%q applied=%q", option.label, selected, applied)
				}
			}
		})
	}
}
