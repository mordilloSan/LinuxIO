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
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "handlers.go" {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(src, []byte("emit.Result(")) || bytes.Contains(src, []byte("emit.Error(")) {
			t.Errorf("%s: use bridgeipc.EmitResult from handlers.go adapters", path)
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.IMPORT {
					t.Errorf("%s: handlers.go must not declare %s blocks; move state/helpers to another file", path, strings.ToLower(decl.Tok.String()))
				}
			case *ast.FuncDecl:
				if decl.Name.Name != "RegisterHandlers" && !strings.HasPrefix(decl.Name.Name, "handle") {
					t.Errorf("%s: unexpected function %s in handlers.go; only RegisterHandlers and handle* adapters are allowed", path, decl.Name.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
