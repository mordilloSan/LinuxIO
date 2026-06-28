package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const dockerMountOrderingDropInPath = "/etc/systemd/system/docker.service.d/linuxio-require-mounts.conf"

func syncDockerServiceMountOrdering(ctx context.Context, docker bridgeconfig.Docker) error {
	if !docker.RequireMountsForFolders {
		return removeDockerMountOrderingDropIn(ctx, dockerMountOrderingDropInPath)
	}

	content, err := renderDockerMountOrderingDropIn(docker.Folders)
	if err != nil {
		return err
	}
	if err := ensureSystemdDropInDir(filepath.Dir(dockerMountOrderingDropInPath)); err != nil {
		return err
	}
	if err := utils.WriteFileAtomic(dockerMountOrderingDropInPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write docker service drop-in: %w", err)
	}
	return systemdapi.DaemonReload(ctx)
}

func ensureSystemdDropInDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create systemd drop-in directory: %w", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("set systemd drop-in directory permissions: %w", err)
	}
	return nil
}

func removeDockerMountOrderingDropIn(ctx context.Context, path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove docker service drop-in: %w", err)
	}
	return systemdapi.DaemonReload(ctx)
}

func renderDockerMountOrderingDropIn(folders []bridgeconfig.AbsolutePath) (string, error) {
	if len(folders) == 0 {
		return "", fmt.Errorf("docker folders cannot be empty")
	}

	var b strings.Builder
	b.WriteString("# Managed by LinuxIO. Do not edit by hand.\n")
	b.WriteString("[Unit]\n")
	b.WriteString("Wants=network-online.target\n")
	b.WriteString("After=network-online.target remote-fs.target\n")
	for _, folder := range folders {
		value := string(folder)
		if strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("docker folder cannot contain a newline")
		}
		fmt.Fprintf(&b, "RequiresMountsFor=%s\n", systemdUnitValue(value))
	}
	return b.String(), nil
}

func systemdUnitValue(value string) string {
	if strings.ContainsAny(value, " \t\"'\\") {
		return strconv.Quote(value)
	}
	return value
}
