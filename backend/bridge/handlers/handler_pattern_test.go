package handlers

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerFilesOnlyContainRegistrationAndAdapters(t *testing.T) {
	if err := walkHandlerFiles(func(path string) error {
		return checkHandlerFile(t, path)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerCodeUsesCallerContextForBlockingWork(t *testing.T) {
	if err := walkGoFiles(func(path string) error {
		return checkContextPropagation(t, path)
	}); err != nil {
		t.Fatal(err)
	}
}

func walkHandlerFiles(check func(path string) error) error {
	return filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipHandlerFile(path, d) {
			return nil
		}
		return check(path)
	})
}

func walkGoFiles(check func(path string) error) error {
	return filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return check(path)
	})
}

func skipHandlerFile(path string, d os.DirEntry) bool {
	return d.IsDir() || filepath.Base(path) != "handlers.go"
}

func checkHandlerFile(t *testing.T, path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	checkHandlerEmitCalls(t, path, src)
	file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		return err
	}
	checkHandlerDecls(t, path, file.Decls)
	return nil
}

func checkHandlerEmitCalls(t *testing.T, path string, src []byte) {
	if bytes.Contains(src, []byte("emit.Result(")) || bytes.Contains(src, []byte("emit.Error(")) {
		t.Errorf("%s: use bridgeipc.EmitResult from handlers.go adapters", path)
	}
}

func checkHandlerDecls(t *testing.T, path string, decls []ast.Decl) {
	for _, decl := range decls {
		checkHandlerDecl(t, path, decl)
	}
}

func checkHandlerDecl(t *testing.T, path string, decl ast.Decl) {
	switch decl := decl.(type) {
	case *ast.GenDecl:
		checkHandlerGenDecl(t, path, decl)
	case *ast.FuncDecl:
		checkHandlerFuncDecl(t, path, decl)
	}
}

func checkHandlerGenDecl(t *testing.T, path string, decl *ast.GenDecl) {
	if decl.Tok != token.IMPORT {
		t.Errorf("%s: handlers.go must not declare %s blocks; move state/helpers to another file", path, strings.ToLower(decl.Tok.String()))
	}
}

func checkHandlerFuncDecl(t *testing.T, path string, decl *ast.FuncDecl) {
	if decl.Name.Name != "RegisterHandlers" && !strings.HasPrefix(decl.Name.Name, "handle") {
		t.Errorf("%s: unexpected function %s in handlers.go; only RegisterHandlers and handle* adapters are allowed", path, decl.Name.Name)
	}
}

func checkContextPropagation(t *testing.T, path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isSelectorCall(call, "exec", "Command") {
				t.Errorf("%s:%s: use exec.CommandContext with caller ctx", path, fn.Name.Name)
			}
			if isSelectorCall(call, "context", "Background") && !isAllowedBackground(path, fn.Name.Name) {
				t.Errorf("%s:%s: use caller ctx instead of context.Background", path, fn.Name.Name)
			}
			return true
		})
	}
	return nil
}

func isSelectorCall(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

func isAllowedBackground(path, funcName string) bool {
	allowed := map[string]map[string]bool{
		"appupdate/app_update_operation.go": {
			"detachedPostUpdateContext": true,
		},
		"docker/docker.go": {
			"detachedDockerStartupContext": true,
		},
		"docker/watchtower.go": {
			"detachedWatchtowerContext": true,
		},
		"filebrowser/filebrowser.go": {
			"runDetachedIndexerUpdate": true,
		},
	}
	return allowed[filepath.ToSlash(path)][funcName]
}
