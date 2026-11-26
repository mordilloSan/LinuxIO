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
	"accounts": {
		"control": "logs the user out through logind when the account is deleted",
		"shares":  "a password set with shared-folder sign-in on is also written to Samba, and a changed user or group has the shared folders' access applied again",
	},
	"control": {"packages": "control.version names the pending update to the linuxio package"},
	"config":  {"docker": "the Docker startup drop-ins (mount ordering, host IP wait) are rewritten when their settings change"},
	"capabilities": {
		"docker": "capability probe", "memory": "capability probe", "monitoring": "the health probe reuses the daemon client",
		"power": "capability probe", "shares": "capability probe", "storage": "capability probe", "virt": "capability probe",
	},
	"docker":      {"indexer": "keeps the file index in sync with container mounts", "monitoring": "container metrics come from the monitoring daemon client"},
	"filebrowser": {"indexer": "search and index maintenance use the indexer client"},
	"memory":      {"power": "the swap status says whether a TuneD profile manages swappiness"},
	"packages":    {"capabilities": "capability registry (CapabilitySpec, InstallSpec)", "docker": "Docker capability probe"},
	"shares":      {"storage": "a shared folder is a pool subvolume with a size limit and a snapshot rule, made and removed through the storage operations"},
	"system":      {"packages": "the health summary counts pending updates", "systemd": "the health summary lists services"},
	"virt":        {"docker": "excludes Docker-owned bridges from VM bridge candidates", "network": "the VM preflight warns when the host firewall drops bridged VM traffic"},
}

// TestImportBoundaries keeps the layering docs/bridge_handler_patterns.md
// describes: handler families are leaves, common never imports a process
// tree, the webserver imports only common, and the bridge reaches the daemons
// only through their api packages. All code, tests included, uses encoding/json/v2, never v1.
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		return checkFileImports(t, root, path, strings.HasSuffix(path, "_test.go"))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFileImports(t *testing.T, root, path string, testFile bool) error {
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
		if dep == "encoding/json" {
			t.Errorf("%s imports encoding/json: use encoding/json/v2 (jsontext.Value for raw JSON)", rel)
			continue
		}
		// Tests may cross layers; only the codec rule applies to them.
		if testFile || !strings.HasPrefix(dep, modulePrefix) {
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
	case pkg == "cli" || pkg == "docker-update" || pkg == "worker" || pkg == "claude-relay":
		if strings.HasPrefix(dep, "common/") {
			return ""
		}
		return "worker binaries import only common"
	}
	return ""
}

func handlerImportViolation(pkg, dep string) string {
	switch {
	case pkg == "bridge/handlers":
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
