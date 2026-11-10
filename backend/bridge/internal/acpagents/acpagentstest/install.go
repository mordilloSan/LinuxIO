// Package acpagentstest lays out fake agent installs for tests.
package acpagentstest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
)

// Install writes what `npm install --prefix` leaves behind for an agent under
// home: the package manifest at the given version and script as the
// executable in node_modules/.bin.
func Install(t testing.TB, home, id string, agent acpagents.Agent, version, script string) {
	t.Helper()
	modules := filepath.Join(acpagents.Dir(home, id), "node_modules")
	pkg := filepath.Join(modules, filepath.FromSlash(agent.Package))
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"`+version+`"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(modules, ".bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modules, ".bin", agent.Bin), []byte(script), 0o755))
}
