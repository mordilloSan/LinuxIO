package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// Info reports one completed change; a present-participle message ("starting",
// "syncing") is a progress line and belongs at Debug.
func TestInfoLogsReportCompletedChanges(t *testing.T) {
	err := walkGoFiles(func(path string) error {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			msg, ok := infoMessage(call)
			if ok && strings.HasSuffix(firstWord(msg), "ing") {
				t.Errorf("%s: slog.Info message %q is a progress line, log it at Debug", fset.Position(call.Pos()), msg)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// infoMessage returns the message literal of a slog.Info/InfoContext call.
func infoMessage(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "slog" {
		return "", false
	}
	idx := 0
	switch sel.Sel.Name {
	case "Info":
	case "InfoContext":
		idx = 1
	default:
		return "", false
	}
	if len(call.Args) <= idx {
		return "", false
	}
	lit, ok := call.Args[idx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	msg, err := strconv.Unquote(lit.Value)
	return msg, err == nil
}

func firstWord(msg string) string {
	word, _, _ := strings.Cut(strings.ToLower(msg), " ")
	return word
}
