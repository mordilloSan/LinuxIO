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
