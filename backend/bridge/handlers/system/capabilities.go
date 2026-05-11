package system

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	nfsshares "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/shares"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
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

func checkedCapability(ok bool, err error, unavailable error) (bool, string) {
	if err != nil {
		return false, err.Error()
	}
	if !ok && unavailable != nil {
		return false, unavailable.Error()
	}
	return ok, ""
}

func capabilityStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}

func logUnavailableCapability(name, message string) {
	if message == "" {
		return
	}
	slog.Info(name+" unavailable.", "error", message)
}

func logCapabilitiesSummary(out capabilitiesResponse) {
	slog.Info(fmt.Sprintf(
		"Capabilities: docker=%s indexer=%s sensors=%s smart=%s packagekit=%s nfs-client=%s nfs-server=%s tuned=%s.",
		capabilityStatus(out.DockerAvailable),
		capabilityStatus(out.IndexerAvailable),
		capabilityStatus(out.LMSensorsAvailable),
		capabilityStatus(out.SmartmontoolsAvailable),
		capabilityStatus(out.PackageKitAvailable),
		capabilityStatus(out.NFSClientAvailable),
		capabilityStatus(out.NFSServerAvailable),
		capabilityStatus(out.TunedAvailable),
	))

	logUnavailableCapability("Docker service", out.DockerError)
	logUnavailableCapability("Indexer service", out.IndexerError)
	logUnavailableCapability("lm-sensors", out.LMSensorsError)
	logUnavailableCapability("smartmontools", out.SmartmontoolsError)
	logUnavailableCapability("PackageKit", out.PackageKitError)
	logUnavailableCapability("NFS client", out.NFSClientError)
	logUnavailableCapability("NFS server", out.NFSServerError)
	logUnavailableCapability("TuneD", out.TunedError)
}

func buildCapabilitiesResponse(ctx context.Context) capabilitiesResponse {
	slog.Info("Checking system capabilities.")

	var out capabilitiesResponse

	ok, err := docker.CheckDockerAvailability()
	out.DockerAvailable, out.DockerError = checkedCapability(ok, err, nil)

	ok, err = filebrowser.CheckIndexerAvailability(ctx)
	out.IndexerAvailable, out.IndexerError = checkedCapability(ok, err, nil)

	ok, err = checkDependencyCommand("sensors", "lm-sensors")
	out.LMSensorsAvailable, out.LMSensorsError = checkedCapability(ok, err, nil)

	ok, err = checkDependencyCommand("smartctl", "smartmontools")
	out.SmartmontoolsAvailable, out.SmartmontoolsError = checkedCapability(ok, err, nil)

	ok, err = dbusclient.PackageKit.Available(ctx)
	out.PackageKitAvailable, out.PackageKitError = checkedCapability(ok, err, dbusclient.ErrPackageKitUnavailable)

	ok, err = storage.CheckNFSClientAvailability()
	out.NFSClientAvailable, out.NFSClientError = checkedCapability(ok, err, nil)

	ok, err = nfsshares.CheckNFSServerAvailability()
	out.NFSServerAvailable, out.NFSServerError = checkedCapability(ok, err, nil)

	ok, err = power.Available()
	out.TunedAvailable, out.TunedError = checkedCapability(ok, err, power.ErrUnavailable)

	logCapabilitiesSummary(out)

	return out
}
