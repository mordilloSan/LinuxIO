package system

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/pkgkit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type capabilitiesResponse struct {
	DockerAvailable        bool   `json:"docker_available"`
	IndexerAvailable       bool   `json:"indexer_available"`
	LMSensorsAvailable     bool   `json:"lm_sensors_available"`
	SmartmontoolsAvailable bool   `json:"smartmontools_available"`
	PackageKitAvailable    bool   `json:"packagekit_available"`
	NFSAvailable           bool   `json:"nfs_available"`
	TunedAvailable         bool   `json:"tuned_available"`
	DockerError            string `json:"docker_error,omitempty"`
	IndexerError           string `json:"indexer_error,omitempty"`
	LMSensorsError         string `json:"lm_sensors_error,omitempty"`
	SmartmontoolsError     string `json:"smartmontools_error,omitempty"`
	PackageKitError        string `json:"packagekit_error,omitempty"`
	NFSError               string `json:"nfs_error,omitempty"`
	TunedError             string `json:"tuned_error,omitempty"`
}

func checkDependencyCommand(command, dependencyName string) (bool, error) {
	if path, err := exec.LookPath(command); err != nil {
		slog.Info(dependencyName + " unavailable")
		return false, fmt.Errorf("%s not found (missing %s dependency)", command, dependencyName)
	} else {
		slog.Info(dependencyName+" available", "path", path)
	}
	return true, nil
}

func checkedCapability(check func() (bool, error), unavailable error) (bool, string) {
	ok, err := check()
	if err != nil {
		return false, err.Error()
	}
	if !ok && unavailable != nil {
		return false, unavailable.Error()
	}
	return ok, ""
}

func commandCapability(command, dependencyName string) (bool, string) {
	return checkedCapability(func() (bool, error) {
		return checkDependencyCommand(command, dependencyName)
	}, nil)
}

func dockerCapability() (bool, string) {
	if _, err := docker.CheckDockerAvailability(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func buildCapabilitiesResponse() capabilitiesResponse {
	var out capabilitiesResponse

	out.DockerAvailable, out.DockerError = dockerCapability()
	out.IndexerAvailable, out.IndexerError = checkedCapability(filebrowser.CheckIndexerAvailability, nil)
	out.LMSensorsAvailable, out.LMSensorsError = commandCapability("sensors", "lm-sensors")
	out.SmartmontoolsAvailable, out.SmartmontoolsError = commandCapability("smartctl", "smartmontools")
	out.PackageKitAvailable, out.PackageKitError = checkedCapability(pkgkit.Available, pkgkit.ErrUnavailable)
	out.NFSAvailable, out.NFSError = checkedCapability(storage.CheckNFSAvailability, nil)
	out.TunedAvailable, out.TunedError = checkedCapability(power.Available, power.ErrUnavailable)

	return out
}

func registerCapabilitiesHandlers() {
	ipc.RegisterFunc("system", "get_capabilities", func(ctx context.Context, args []string, emit ipc.Events) error {
		return emit.Result(buildCapabilitiesResponse())
	})
}
