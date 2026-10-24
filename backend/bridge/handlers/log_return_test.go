package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestHandlersReturnOrLogErrorsNotBoth enforces "log or return, never both".
// The bridge router and task runner log every failed route and task exactly
// once, so a handler that logs an error and then returns it writes the same
// failure to the journal twice.
func TestHandlersReturnOrLogErrorsNotBoth(t *testing.T) {
	if err := walkGoFiles(func(path string) error {
		return checkLogThenReturn(t, path)
	}); err != nil {
		t.Fatal(err)
	}
}

func checkLogThenReturn(t *testing.T, path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return err
	}
	ast.Inspect(file, func(n ast.Node) bool {
		var fn *ast.FuncType
		var body *ast.BlockStmt
		switch f := n.(type) {
		case *ast.FuncDecl:
			fn, body = f.Type, f.Body
		case *ast.FuncLit:
			fn, body = f.Type, f.Body
		default:
			return true
		}
		errIdx, nres := errorResultIndex(fn)
		if body == nil || errIdx < 0 {
			return true
		}
		ast.Inspect(body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false // visited on its own by the outer walk
			}
			if block, ok := n.(*ast.BlockStmt); ok {
				checkLogThenReturnBlock(t, fset, path, block, errIdx, nres)
			}
			return true
		})
		return true
	})
	return nil
}

// checkLogThenReturnBlock flags a Warn/Error log followed later in the same
// block (or directly after the statement that contains it) by a return of the
// logged error. A return of a different named error is fine, as is a log whose
// error is swallowed.
func checkLogThenReturnBlock(t *testing.T, fset *token.FileSet, path string, block *ast.BlockStmt, errIdx, nres int) {
	var logged map[string]bool
	logLine := 0
	for i, stmt := range block.List {
		if idents, ok := errorLogCall(stmt); ok {
			logged, logLine = idents, fset.Position(stmt.Pos()).Line
			continue
		}
		if i+1 < len(block.List) {
			if ret, ok := block.List[i+1].(*ast.ReturnStmt); ok {
				if idents, ok := nestedErrorLog(stmt); ok {
					reportLogThenReturn(t, fset, path, idents, fset.Position(stmt.Pos()).Line, ret, errIdx, nres, true)
				}
			}
		}
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || logged == nil {
			continue
		}
		reportLogThenReturn(t, fset, path, logged, logLine, ret, errIdx, nres, false)
	}
}

// reportLogThenReturn fails when ret returns the error the log mentioned. A
// return of a different named error is fine, and so is a log nested in an
// if/for body whose error is swallowed before an unrelated return.
func reportLogThenReturn(t *testing.T, fset *token.FileSet, path string, logged map[string]bool, logLine int, ret *ast.ReturnStmt, errIdx, nres int, nested bool) {
	expr := returnedError(ret, errIdx, nres)
	if expr == nil {
		return
	}
	returned := errorIdents(expr)
	sameError := false
	for name := range returned {
		if logged[name] {
			sameError = true
		}
	}
	if !sameError && (nested || (len(returned) > 0 && len(logged) > 0)) {
		return
	}
	t.Errorf("%s:%d logs an error and line %d returns it: log or return, not both (the router logs failed routes)",
		path, logLine, fset.Position(ret.Pos()).Line)
}

var errorLogMethods = map[string]bool{"Error": true, "Warn": true, "ErrorContext": true, "WarnContext": true}

// errorLogCall reports whether stmt is a slog-style Warn/Error call and which
// error-like identifiers its arguments mention.
func errorLogCall(stmt ast.Stmt) (map[string]bool, bool) {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !errorLogMethods[sel.Sel.Name] {
		return nil, false
	}
	for i, arg := range call.Args {
		if i > 1 {
			break
		}
		if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			idents := map[string]bool{}
			for _, arg := range call.Args {
				for name := range errorIdents(arg) {
					idents[name] = true
				}
			}
			return idents, true
		}
	}
	return nil, false
}

// nestedErrorLog finds a Warn/Error log inside stmt (an if, for or switch body).
func nestedErrorLog(stmt ast.Stmt) (map[string]bool, bool) {
	var found map[string]bool
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if s, ok := n.(ast.Stmt); ok {
			if idents, ok := errorLogCall(s); ok {
				found = idents
				return false
			}
		}
		return true
	})
	return found, found != nil
}

// errorIdents collects identifiers in expr whose name looks like an error.
func errorIdents(expr ast.Expr) map[string]bool {
	idents := map[string]bool{}
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(strings.ToLower(id.Name), "err") && id.Name != "nil" {
			idents[id.Name] = true
		}
		return true
	})
	return idents
}

// errorResultIndex returns the position of the `error` result and the number
// of results, or -1 when the function does not return an error.
func errorResultIndex(fn *ast.FuncType) (int, int) {
	if fn.Results == nil {
		return -1, 0
	}
	idx, n := -1, 0
	for _, field := range fn.Results.List {
		count := max(len(field.Names), 1)
		for range count {
			if id, ok := field.Type.(*ast.Ident); ok && id.Name == "error" && idx < 0 {
				idx = n
			}
			n++
		}
	}
	return idx, n
}

// returnedError returns the expression in the error position of ret, or nil
// when it is a literal nil or ret cannot be inspected (a naked return).
func returnedError(ret *ast.ReturnStmt, errIdx, nres int) ast.Expr {
	if len(ret.Results) == 0 {
		return nil
	}
	if len(ret.Results) == 1 && nres > 1 {
		return ret.Results[0] // return f()
	}
	if errIdx >= len(ret.Results) {
		return nil
	}
	expr := ret.Results[errIdx]
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return nil
	}
	return expr
}
