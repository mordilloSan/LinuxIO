package system

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type capabilitiesResponse struct {
	DockerAvailable        bool   `json:"docker_available"`
	IndexerAvailable       bool   `json:"indexer_available"`
	LMSensorsAvailable     bool   `json:"lm_sensors_available"`
	SmartmontoolsAvailable bool   `json:"smartmontools_available"`
	DockerError            string `json:"docker_error,omitempty"`
	IndexerError           string `json:"indexer_error,omitempty"`
	LMSensorsError         string `json:"lm_sensors_error,omitempty"`
	SmartmontoolsError     string `json:"smartmontools_error,omitempty"`
}

func checkDependencyCommand(command, dependencyName string) (bool, error) {
	if path, err := exec.LookPath(command); err != nil {
		logger.Infof("%s dependency unavailable", dependencyName)
		return false, fmt.Errorf("%s not found (missing %s dependency)", command, dependencyName)
	} else {
		logger.Infof("%s dependency available", dependencyName)
		logger.Debugf("%s found at %s", dependencyName, path)
	}
	return true, nil
}

func registerCapabilitiesHandlers() {
	ipc.RegisterFunc("system", "get_capabilities", func(ctx context.Context, args []string, emit ipc.Events) error {
		var out capabilitiesResponse

		if _, err := docker.CheckDockerAvailability(); err != nil {
			out.DockerAvailable = false
			out.DockerError = err.Error()
		} else {
			out.DockerAvailable = true
		}

		if ok, err := filebrowser.CheckIndexerAvailability(); err != nil {
			out.IndexerAvailable = false
			out.IndexerError = err.Error()
		} else {
			out.IndexerAvailable = ok
		}

		if ok, err := checkDependencyCommand("sensors", "lm-sensors"); err != nil {
			out.LMSensorsAvailable = false
			out.LMSensorsError = err.Error()
		} else {
			out.LMSensorsAvailable = ok
		}

		if ok, err := checkDependencyCommand("smartctl", "smartmontools"); err != nil {
			out.SmartmontoolsAvailable = false
			out.SmartmontoolsError = err.Error()
		} else {
			out.SmartmontoolsAvailable = ok
		}

		return emit.Result(out)
	})
}
