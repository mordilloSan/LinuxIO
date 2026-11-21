package appupdate

import (
	"log/slog"
	"net/http"

	ver "github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

// readComponentVersions reads the versions the installer recorded; tests
// swap it.
var readComponentVersions = func() (map[string]string, error) {
	return ver.ReadComponentVersions(ver.ComponentsFile)
}

// getComponentVersions returns each program's version as the installer
// recorded it, and the webserver's own from this build. It runs no program:
// /api/version is unauthenticated, and several programs are root only.
func getComponentVersions() map[string]string {
	components, err := readComponentVersions()
	if err != nil {
		slog.Debug("failed to read component versions", "path", ver.ComponentsFile, "error", err)
		components = map[string]string{}
	}
	if ver.Version != "" {
		components[webserverComponent] = ver.Version
	}
	return components
}

// webserverComponent is the webserver's name in ver.Components.
const webserverComponent = "LinuxIO Web Server"

// handleVersion returns installed component versions (public endpoint, no auth required).
// The page reads it to see that the webserver is back after a restart.
func handleVersion(w http.ResponseWriter, _ *http.Request) {
	web.WriteJSON(w, http.StatusOK, getComponentVersions())
}
