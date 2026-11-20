// Package logguard holds the AST checks behind the backend logging contract.
// It is used only by tests.
package logguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// LogThenReturn returns one "file:line: reason" per site in path that logs an
// error at Warn/Error and then returns it.
func LogThenReturn(path string) ([]string, error) {
	var found []string
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
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
				checkLogThenReturnBlock(&found, fset, path, block, errIdx, nres)
			}
			return true
		})
		return true
	})
	return found, nil
}

// checkLogThenReturnBlock flags a Warn/Error log followed later in the same
// block (or directly after the statement that contains it) by a return of the
// logged error. A return of a different named error is fine, as is a log whose
// error is swallowed.
func checkLogThenReturnBlock(found *[]string, fset *token.FileSet, path string, block *ast.BlockStmt, errIdx, nres int) {
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
					reportLogThenReturn(found, fset, path, idents, fset.Position(stmt.Pos()).Line, ret, errIdx, nres, true)
				}
			}
		}
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || logged == nil {
			continue
		}
		reportLogThenReturn(found, fset, path, logged, logLine, ret, errIdx, nres, false)
	}
}

// reportLogThenReturn fails when ret returns the error the log mentioned. A
// return of a different named error is fine, and so is a log nested in an
// if/for body whose error is swallowed before an unrelated return.
func reportLogThenReturn(found *[]string, fset *token.FileSet, path string, logged map[string]bool, logLine int, ret *ast.ReturnStmt, errIdx, nres int, nested bool) {
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
	*found = append(*found, fmt.Sprintf("%s:%d: logs an error and line %d returns it: log or return, not both (the router logs failed routes)",
		path, logLine, fset.Position(ret.Pos()).Line))
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

// acronyms are the product names and acronyms a log message may start with.
var acronyms = []string{"NFS", "CIFS", "SMB", "LVM", "NVML", "PAM", "TLS", "HTTP", "HTTPS", "WebSocket", "WireGuard", "LinuxIO", "Docker", "Btrfs", "SMART", "GPU", "CPU", "PCI", "D-Bus", "NTP", "SSH", "QR", "UI", "API", "PackageKit", "Compose", "Netplan", "NetworkManager", "VNC", "PTY", "JSON", "YAML", "ZFS", "IPv4", "IPv6", "DNS", "MAC", "IP", "systemd", "journald", "yamux", "SSE", "FTS5", "GitHub", "Samba", "SIGHUP"}

// containerNamedPaths are the folders whose log messages about one container
// end with its name, as `"<text> container " + name`, and keep the
// "container" attribute: an exception to constant messages for the Docker
// update pass, which runs from a timer, since the saved output of a run
// (journalctl -o short-iso) shows a message without its attributes.
var containerNamedPaths = []string{"common/dockerupdate/"}

var (
	logLevels   = map[string]bool{"Debug": true, "Info": true, "Warn": true, "Error": true}
	prefixRe    = regexp.MustCompile(`^[A-Za-z]+: `)
	slogAttrFns = []string{"String", "Int", "Int64", "Uint64", "Float64", "Bool", "Time", "Duration", "Any", "Group"}
)

// MessageStyle returns one "file:line: reason" per slog call in path that
// breaks the message contract: constant lowercase text (or a leading entry of
// acronyms), no "[tag]" or "Prefix: ", no trailing punctuation, no value
// built into the message (a line about one container under
// containerNamedPaths ends with its name), and the error attribute key is
// "error", not "err".
// It checks slog.<Level>[Context] and <Level>[Context] on a receiver named
// logger; a *slog.Logger under any other name is not recognised.
func MessageStyle(path string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	namesContainers := slices.ContainsFunc(containerNamedPaths, func(dir string) bool {
		return strings.Contains(filepath.ToSlash(path), dir)
	})
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		msgIdx, ok := logMessageIndex(call)
		if !ok || msgIdx >= len(call.Args) {
			return true
		}
		report := func(pos token.Pos, reason string) {
			found = append(found, fmt.Sprintf("%s:%d: %s", path, fset.Position(pos).Line, reason))
		}
		msg, attrs := call.Args[msgIdx], call.Args[msgIdx+1:]
		if prefix, name, ok := containerNamedMessage(msg); ok && namesContainers {
			if reason := literalViolation(prefix); reason != "" {
				report(msg.Pos(), reason)
			} else if value, ok := attrValue(attrs, "container"); !ok || types.ExprString(value) != types.ExprString(name) {
				report(msg.Pos(), `message ends with a container's name: the "container" attribute must hold the same expression`)
			}
		} else if reason := messageViolation(msg); reason != "" {
			report(msg.Pos(), reason)
		}
		checkAttrKeys(attrs, report)
		return true
	})
	return found, nil
}

// containerNamedMessage reports whether msg is a constant text ending in
// "container " followed by one value, the container's name, and returns the
// text and the name.
func containerNamedMessage(msg ast.Expr) (string, ast.Expr, bool) {
	sum, ok := msg.(*ast.BinaryExpr)
	if !ok || sum.Op != token.ADD {
		return "", nil, false
	}
	lit, ok := sum.X.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", nil, false
	}
	switch sum.Y.(type) {
	case *ast.BasicLit, *ast.BinaryExpr:
		return "", nil, false
	}
	text, err := strconv.Unquote(lit.Value)
	if err != nil || !strings.HasSuffix(text, "container ") {
		return "", nil, false
	}
	return text, sum.Y, true
}

// attrValue returns the value of the attribute key in a log call's attribute
// list. A slog.String(...)-style call is one attribute; anything else is a
// key followed by its value, a key that is not a literal included.
func attrValue(args []ast.Expr, key string) (ast.Expr, bool) {
	quoted := strconv.Quote(key)
	for i := 0; i < len(args); i++ {
		if call, ok := args[i].(*ast.CallExpr); ok && isSlogAttr(call) {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == quoted && len(call.Args) > 1 {
				return call.Args[1], true
			}
			continue
		}
		if lit, ok := args[i].(*ast.BasicLit); ok && lit.Value == quoted && i+1 < len(args) {
			return args[i+1], true
		}
		i++ // skip the value
	}
	return nil, false
}

// isSlogAttr reports whether call builds one attribute, as slog.String does.
func isSlogAttr(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "slog" && slices.Contains(slogAttrFns, sel.Sel.Name)
}

// logMessageIndex reports whether call is a log call and where its message
// argument sits (after ctx for the Context variants).
func logMessageIndex(call *ast.CallExpr) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok || (recv.Name != "slog" && recv.Name != "logger") {
		return 0, false
	}
	level, isCtx := strings.CutSuffix(sel.Sel.Name, "Context")
	if !logLevels[level] {
		return 0, false
	}
	if isCtx {
		return 1, true
	}
	return 0, true
}

// messageViolation returns why the message argument breaks the contract, or "".
func messageViolation(arg ast.Expr) string {
	switch e := arg.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return "message is built with +: keep it constant and pass values as attributes"
		}
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Sprint") {
				return "message is built with fmt." + sel.Sel.Name + ": keep it constant and pass values as attributes"
			}
		}
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return ""
		}
		msg, err := strconv.Unquote(e.Value)
		if err != nil {
			return ""
		}
		return literalViolation(msg)
	}
	return ""
}

func literalViolation(msg string) string {
	if msg == "" {
		return ""
	}
	word, _, _ := strings.Cut(msg, " ")
	word = strings.TrimRightFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	first := []rune(msg)[0]
	switch {
	case strings.HasPrefix(msg, "["):
		return fmt.Sprintf("message %q starts with a [tag]: put the component in an attribute", msg)
	case prefixRe.MatchString(msg):
		return fmt.Sprintf("message %q starts with a \"Prefix: \" tag: put the component in an attribute", msg)
	case !unicode.IsUpper(first):
	case !slices.Contains(acronyms, word):
		return fmt.Sprintf("message %q starts with a capital letter", msg)
	}
	if strings.HasSuffix(msg, ".") || strings.HasSuffix(msg, ":") || strings.HasSuffix(msg, "!") {
		return fmt.Sprintf("message %q ends with punctuation", msg)
	}
	return ""
}

// checkAttrKeys walks the variadic attribute list: a string literal is a key
// followed by its value, a slog.String(...)-style call carries its own key.
func checkAttrKeys(args []ast.Expr, report func(token.Pos, string)) {
	for i := 0; i < len(args); i++ {
		switch a := args[i].(type) {
		case *ast.BasicLit:
			if a.Kind == token.STRING && a.Value == `"err"` {
				report(a.Pos(), `attribute key "err": use "error"`)
			}
			i++ // skip the value
		case *ast.CallExpr:
			sel, ok := a.Fun.(*ast.SelectorExpr)
			if !ok || len(a.Args) == 0 {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "slog" && slices.Contains(slogAttrFns, sel.Sel.Name) {
				if lit, ok := a.Args[0].(*ast.BasicLit); ok && lit.Value == `"err"` {
					report(lit.Pos(), `attribute key "err": use "error"`)
				}
			}
		}
	}
}
