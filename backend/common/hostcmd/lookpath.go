package hostcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SearchDirs are the sbin/bin locations consulted when a command is not on the
// bridge's PATH. Admin tools (btrfs, mount.nfs, exportfs, smbd, ...) usually
// live in /usr/sbin or /sbin, which the service environment may omit.
var SearchDirs = []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"}

// LookPath resolves name via PATH, falling back to SearchDirs. It returns
// exec.ErrNotFound when the command is absent.
func LookPath(name string) (string, error) {
	return LookPathIn(name, SearchDirs)
}

// LookPathIn is LookPath with an explicit fallback directory list.
func LookPathIn(name string, dirs []string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		return exec.LookPath(name)
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}
