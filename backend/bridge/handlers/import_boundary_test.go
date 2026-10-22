package handlers

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePrefix = "github.com/mordilloSan/LinuxIO/backend/"

// allowedHandlerEdges lists the sibling imports a handler family keeps, with
// the reason. Anything else two families need is a library and belongs in
// bridge/internal (bridge-only) or common (shared with another binary).
var allowedHandlerEdges = map[string]map[string]string{
	"accounts":    {"control": "logs the user out through logind when the account is deleted"},
	"docker":      {"indexer": "keeps the file index in sync with container mounts", "monitoring": "container metrics come from the monitoring daemon client"},
	"filebrowser": {"indexer": "search and index maintenance use the indexer client"},
	"packages":    {"docker": "Docker capability probe", "system": "capability registry (CapabilitySpec, InstallSpec)"},
	"system": {
		"docker": "capability probe", "power": "capability probe", "shares": "capability probe",
		"storage": "capability probe", "virt": "capability probe",
		"systemd": "the health summary lists services",
	},
	"virt": {"docker": "excludes Docker-owned bridges from VM bridge candidates"},
}

// shortcut: the root docker-update worker still links handlers/docker, drop
// this once the Docker update engine leaves the handler package.
var handlerImportersOutsideBridge = map[string]string{
	"docker-update": "bridge/handlers/docker",
}

// TestImportBoundaries keeps the layering docs/bridge_handler_patterns.md
// describes: handler families are leaves, common never imports a process
// tree, the webserver imports only common, and the bridge reaches the daemons
// only through their api packages.
func TestImportBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return checkFileImports(t, root, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFileImports(t *testing.T, root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	pkg := filepath.ToSlash(filepath.Dir(rel))
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return err
	}
	for _, imp := range file.Imports {
		dep := strings.Trim(imp.Path.Value, `"`)
		if !strings.HasPrefix(dep, modulePrefix) {
			continue
		}
		dep = strings.TrimPrefix(dep, modulePrefix)
		if reason := importViolation(pkg, dep); reason != "" {
			t.Errorf("%s imports %s: %s", rel, dep, reason)
		}
	}
	return nil
}

func importViolation(pkg, dep string) string {
	switch {
	case strings.HasPrefix(dep, "bridge/handlers"):
		return handlerImportViolation(pkg, dep)
	case strings.HasPrefix(pkg, "common/"):
		if strings.HasPrefix(dep, "common/") {
			return ""
		}
		return "common never imports a process tree"
	case strings.HasPrefix(pkg, "webserver"):
		if strings.HasPrefix(dep, "common/") || strings.HasPrefix(dep, "webserver") {
			return ""
		}
		return "the webserver imports only common and its own packages"
	case strings.HasPrefix(pkg, "indexer") || strings.HasPrefix(pkg, "monitoring"):
		tree, _, _ := strings.Cut(pkg, "/")
		if strings.HasPrefix(dep, "common/") || strings.HasPrefix(dep, tree+"/") {
			return ""
		}
		return "daemons import only common and their own tree"
	case strings.HasPrefix(pkg, "bridge/"):
		if strings.HasPrefix(dep, "bridge/") || strings.HasPrefix(dep, "common/") || dep == "indexer/api" || dep == "monitoring/api" {
			return ""
		}
		return "the bridge reaches the daemons only through their api packages"
	case pkg == "cli" || pkg == "docker-update":
		if strings.HasPrefix(dep, "common/") {
			return ""
		}
		return "worker binaries import only common"
	}
	return ""
}

func handlerImportViolation(pkg, dep string) string {
	switch {
	case pkg == "bridge/handlers", handlerImportersOutsideBridge[pkg] == dep:
		return ""
	case dep == "bridge/handlers" && (pkg == "bridge/cmd" || pkg == "bridge/internal/apigen"):
		return "" // the composition root and the contract generator read the registry
	case !strings.HasPrefix(pkg, "bridge/handlers/"):
		return "handler packages are leaves; nothing outside bridge/handlers imports them"
	}
	from, to := handlerFamily(pkg), handlerFamily(dep)
	if to == "" {
		return "handler families do not import the registry"
	}
	if from == to || allowedHandlerEdges[from][to] != "" {
		return ""
	}
	return "sibling handler families only import each other through allowedHandlerEdges; move shared code to bridge/internal or common"
}

func handlerFamily(pkg string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(pkg, "bridge/handlers"), "/")
	family, _, _ := strings.Cut(rest, "/")
	return family
}
