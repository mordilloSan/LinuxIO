package shares

import (
	"os"
	"os/exec"
	"path/filepath"
)

// serverCommandSearchDirs are the sbin/bin locations consulted when a share
// server command (exportfs, smbd, etc.) is not on the bridge's PATH. System
// daemons frequently live in /usr/sbin or /sbin, which are not always exported
// to the environment the bridge runs under.
var serverCommandSearchDirs = []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"}

// findServerCommand resolves command via PATH, falling back to the well-known
// sbin/bin directories. Returns exec.ErrNotFound when the command is absent.
func findServerCommand(command string) (string, error) {
	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}
	for _, dir := range serverCommandSearchDirs {
		path := filepath.Join(dir, command)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}
