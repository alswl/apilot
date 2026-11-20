package workbench

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alswl/apilot/core/pkg/config"
)

func TestDocumentedKeybindingProfiles(t *testing.T) {
	for _, profile := range []string{"example", "vim"} {
		t.Run(profile, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "..", "docs", "keybindings."+profile+".json")
			settings, err := config.ReadKeybindings(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := compileBindings(settings); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
		})
	}
}

func TestWorkbenchUILiterals(t *testing.T) {
	files := token.NewFileSet()
	packages, err := parser.ParseDir(files, ".", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for path, file := range packages["workbench"].Files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch function := call.Fun.(type) {
			case *ast.Ident:
				if function.Name == "tr" && len(call.Args) == 1 {
					if key, ok := stringLiteral(call.Args[0]); ok && strings.TrimSpace(english[key]) == "" {
						t.Errorf("%s: missing English translation for %q", files.Position(call.Pos()), key)
					}
				}
			case *ast.SelectorExpr:
				pkg, ok := function.X.(*ast.Ident)
				if ok && pkg.Name == "components" && (function.Sel.Name == "IconButton" || function.Sel.Name == "IconToggle") && len(call.Args) >= 3 {
					if label, ok := stringLiteral(call.Args[2]); ok && strings.TrimSpace(label) == "" {
						t.Errorf("%s: %s requires an action label", files.Position(call.Pos()), function.Sel.Name)
					}
				}
			}
			return true
		})
	}
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}
