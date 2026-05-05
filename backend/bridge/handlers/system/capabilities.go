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
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type capabilitiesResponse struct {
	DockerAvailable        bool   `json:"docker_available"`
	IndexerAvailable       bool   `json:"indexer_available"`
	LMSensorsAvailable     bool   `json:"lm_sensors_available"`
	SmartmontoolsAvailable bool   `json:"smartmontools_available"`
	PackageKitAvailable    bool   `json:"packagekit_available"`
	NFSClientAvailable     bool   `json:"nfs_client_available"`
	NFSServerAvailable     bool   `json:"nfs_server_available"`
	TunedAvailable         bool   `json:"tuned_available"`
	DockerError            string `json:"docker_error,omitempty"`
	IndexerError           string `json:"indexer_error,omitempty"`
	LMSensorsError         string `json:"lm_sensors_error,omitempty"`
	SmartmontoolsError     string `json:"smartmontools_error,omitempty"`
	PackageKitError        string `json:"packagekit_error,omitempty"`
	NFSClientError         string `json:"nfs_client_error,omitempty"`
	NFSServerError         string `json:"nfs_server_error,omitempty"`
	TunedError             string `json:"tuned_error,omitempty"`
}

func checkDependencyCommand(command, dependencyName string) (bool, error) {
	if _, err := exec.LookPath(command); err != nil {
		return false, fmt.Errorf("%s not found (missing %s dependency)", command, dependencyName)
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

func loggedCapability(name string, check func() (bool, error), unavailable error) (bool, string) {
	ok, message := checkedCapability(check, unavailable)
	if ok {
		slog.Info(name + " available")
		return ok, message
	}
	if message != "" {
		slog.Info(name+" unavailable", "error", message)
	} else {
		slog.Info(name + " unavailable")
	}
	return ok, message
}

func dependencyCommandCheck(command, dependencyName string) func() (bool, error) {
	return func() (bool, error) {
		return checkDependencyCommand(command, dependencyName)
	}
}

func buildCapabilitiesResponse() capabilitiesResponse {
	slog.Info("checking system capabilities")

	var out capabilitiesResponse

	out.DockerAvailable, out.DockerError = loggedCapability("docker service", docker.CheckDockerAvailability, nil)
	out.IndexerAvailable, out.IndexerError = loggedCapability("indexer service", filebrowser.CheckIndexerAvailability, nil)
	out.LMSensorsAvailable, out.LMSensorsError = loggedCapability("lm-sensors", dependencyCommandCheck("sensors", "lm-sensors"), nil)
	out.SmartmontoolsAvailable, out.SmartmontoolsError = loggedCapability("smartmontools", dependencyCommandCheck("smartctl", "smartmontools"), nil)
	out.PackageKitAvailable, out.PackageKitError = loggedCapability("PackageKit", pkgkit.Available, pkgkit.ErrUnavailable)
	out.NFSClientAvailable, out.NFSClientError = loggedCapability("NFS client", storage.CheckNFSClientAvailability, nil)
	out.NFSServerAvailable, out.NFSServerError = loggedCapability("NFS server", nfsshares.CheckNFSServerAvailability, nil)
	out.TunedAvailable, out.TunedError = loggedCapability("TuneD", power.Available, power.ErrUnavailable)

	return out
}

func registerCapabilitiesHandlers() {
	ipc.RegisterFunc("system", "get_capabilities", func(ctx context.Context, args []string, emit ipc.Events) error {
		return emit.Result(buildCapabilitiesResponse())
	})
}
