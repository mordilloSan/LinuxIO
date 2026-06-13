package watchtower

import (
	"fmt"
	"os"
)

// CheckInstalled reports whether the LinuxIO-managed Watchtower install is
// complete: the binary plus the env, unit, and timer files.
func CheckInstalled() (bool, error) {
	return checkInstall(installPaths{
		binary: BinaryPath(),
		env:    EnvPath,
		unit:   UnitPath,
		timer:  TimerPath,
	})
}

type installPaths struct {
	binary string
	env    string
	unit   string
	timer  string
}

func checkInstall(paths installPaths) (bool, error) {
	if err := requireExecutableFile(paths.binary); err != nil {
		return false, err
	}
	if err := requireRegularFile(paths.env); err != nil {
		return false, err
	}
	if err := requireRegularFile(paths.unit); err != nil {
		return false, err
	}
	if err := requireRegularFile(paths.timer); err != nil {
		return false, err
	}
	return true, nil
}

func requireExecutableFile(path string) error {
	info, err := requireFileInfo(path)
	if err != nil {
		return err
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func requireRegularFile(path string) error {
	_, err := requireFileInfo(path)
	return err
}

func requireFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s not found", path)
		}
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return info, nil
}
