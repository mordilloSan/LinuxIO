package network

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ExecRunner struct{}

func (ExecRunner) LookPath(name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
		return "", fmt.Errorf("%s not found", name)
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"} {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}

func (r ExecRunner) Run(name string, args ...string) ([]byte, error) {
	path, err := r.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...).CombinedOutput()
}
