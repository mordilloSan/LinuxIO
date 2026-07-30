package handlers

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
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

// Why a route can still be on HandleEvents. Every value except progressEmitter
// is a defect to pay down, not a design choice.
const (
	// The handler emits progress or data frames, so it needs the raw emitter.
	// This is the only legitimate reason.
	progressEmitter = "progress-emitter"
	// The domain function returns bare `any`, so the route's declared Result is
	// checked against nothing. Tighten the signature — it usually already builds
	// the declared type and widens it at the return.
	anyDomain = "any-domain"
	// The domain function returns map[string]any where the route declares a
	// struct. Where the keys already match, this is a struct literal away.
	mapVsStruct = "map-vs-struct"
	// A handler package keeps its own copy of an apischema model and returns
	// that. Resolving this means picking one type, not converting between them.
	duplicateModel = "duplicate-model"
)

// handleEventsInventory is the single record of remaining contract drift — it
// replaces the per-file prose that used to describe the same four facts twenty
// times over and could go stale silently. Every route bound with HandleEvents
// must appear here with its reason, and every entry must still be bound that way:
// TestHandleEventsInventoryIsCurrent fails on either mismatch, so this table
// cannot drift from the code the way comments did.
//
// It doubles as the ratchet. To pay one down, tighten the domain signature, move
// the binding to Handle/HandleVoid, and delete its line.
var handleEventsInventory = map[string]string{
	"accounts.get_user_details":   duplicateModel,
	"accounts.list_groups":        duplicateModel,
	"accounts.list_user_logins":   duplicateModel,
	"config.get":                  duplicateModel,
	"control.version":             duplicateModel,
	"docker.delete_compose_stack": anyDomain,
	"docker.delete_stack":         anyDomain,
	"docker.get_icon":             duplicateModel,
	"docker.get_icon_info":        duplicateModel,
	"docker.get_icon_uri":         duplicateModel,
	"docker.list_networks":        anyDomain,
	"docker.list_volumes":         anyDomain,
	"docker.normalize_compose":    anyDomain,
	"filebrowser.indexer_status":  anyDomain,
	"filebrowser.resource_patch":  progressEmitter,
	"filebrowser.search":          anyDomain,
	"filebrowser.subfolders":      anyDomain,
	"indexer.get_status":          duplicateModel,
	"network.get_network_info":    duplicateModel,
	"storage.get_drive_info":      duplicateModel,
	"system.get_host_info":        duplicateModel,
	"system.get_services":         anyDomain,
	"system.get_uptime":           duplicateModel,
	"systemd.get_unit_info":       mapVsStruct,
	"systemd.list_services":       duplicateModel,
	"systemd.list_sockets":        duplicateModel,
	"systemd.list_timers":         duplicateModel,
	"updates.get_update_detail":   duplicateModel,
	"updates.get_updates_basic":   duplicateModel,
	"virt.create":                 progressEmitter,
	"wireguard.list_interfaces":   anyDomain,
	"wireguard.list_peers":        anyDomain,
}

// handleEventsRoute finds the route name a `.HandleEvents(` binding belongs to by
// taking the nearest preceding route literal. Options like apischema.Privileged()
// sit between the two, so a single forward regex misses those bindings.
var routeLiteral = regexp.MustCompile(`"([a-z_]+\.[a-z_0-9]+)"`)

func TestHandleEventsInventoryIsCurrent(t *testing.T) {
	bound := map[string]bool{}
	total := 0
	if err := walkGoFiles(func(path string) error {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for offset := 0; ; {
			idx := bytes.Index(src[offset:], []byte(".HandleEvents("))
			if idx < 0 {
				return nil
			}
			total++
			if m := routeLiteral.FindAllStringSubmatch(string(src[:offset+idx]), -1); len(m) > 0 {
				bound[m[len(m)-1][1]] = true
			}
			offset += idx + 1
		}
	}); err != nil {
		t.Fatal(err)
	}

	for route := range bound {
		if _, ok := handleEventsInventory[route]; !ok {
			t.Errorf("%s is bound with HandleEvents but is not in handleEventsInventory: add it with the reason, or bind it with Handle/HandleVoid", route)
		}
	}
	for route := range handleEventsInventory {
		if !bound[route] {
			t.Errorf("%s is in handleEventsInventory but no longer bound with HandleEvents: delete its line", route)
		}
	}
	if total != len(handleEventsInventory) {
		t.Errorf("found %d HandleEvents bindings but the inventory lists %d", total, len(handleEventsInventory))
	}
}

func TestTypedBindingsAreTheDefault(t *testing.T) {
	counts := map[string]int{}
	if err := walkGoFiles(func(path string) error {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, form := range []string{".HandleEvents(", ".HandleVoid(", ".Handle("} {
			counts[form] += bytes.Count(src, []byte(form))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The typed forms must stay the overwhelming default; the inventory above is
	// the authority on the exact HandleEvents set.
	if typed := counts[".Handle("] + counts[".HandleVoid("]; typed < counts[".HandleEvents("] {
		t.Errorf("typed bindings (%d) should outnumber raw-emitter bindings (%d)", typed, counts[".HandleEvents("])
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
	if decl.Tok == token.IMPORT {
		return
	}
	if decl.Tok == token.VAR && isAllowedRouteDecl(decl) {
		return
	}
	t.Errorf("%s: handlers.go must not declare %s blocks except route specs; move state/helpers to another file", path, strings.ToLower(decl.Tok.String()))
}

func isAllowedRouteDecl(decl *ast.GenDecl) bool {
	for _, spec := range decl.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			return false
		}
		for _, name := range value.Names {
			if name.Name != "api" && name.Name != "routes" && name.Name != "Routes" && !strings.HasPrefix(name.Name, "Route") {
				return false
			}
		}
	}
	return true
}

func checkHandlerFuncDecl(t *testing.T, path string, decl *ast.FuncDecl) {
	if decl.Name.Name != "RegisterHandlers" && decl.Name.Name != "routeBindings" && !strings.HasPrefix(decl.Name.Name, "handle") {
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
		"filebrowser/filebrowser.go": {
			"runDetachedIndexerUpdate": true,
		},
	}
	return allowed[filepath.ToSlash(path)][funcName]
}
