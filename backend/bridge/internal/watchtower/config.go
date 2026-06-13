package watchtower

import (
	"embed"
	"fmt"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	BinaryName     = "linuxio-watchtower"
	UnitName       = "linuxio-watchtower.service"
	TimerName      = "linuxio-watchtower.timer"
	EnvPath        = "/etc/linuxio/watchtower.env"
	UnitPath       = "/etc/systemd/system/" + UnitName
	TimerPath      = "/etc/systemd/system/" + TimerName
	NoContainersID = "__linuxio_no_containers_selected__"
)

//go:embed watchtower.env linuxio-watchtower.service linuxio-watchtower.timer
var files embed.FS

func BinaryPath() string {
	return filepath.Join(version.BinDir, BinaryName)
}

func UnitFile() ([]byte, error) {
	return embeddedFile("linuxio-watchtower.service")
}

func TimerFile() ([]byte, error) {
	return RenderTimer(DefaultScheduleTime)
}

func DefaultEnvFile() ([]byte, error) {
	return RenderEnv(DefaultScheduleConfig())
}

func embeddedFile(path string) ([]byte, error) {
	data, err := files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded watchtower file %s: %w", path, err)
	}
	return append([]byte(nil), data...), nil
}
