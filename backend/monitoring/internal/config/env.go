package config

import (
	"os"
)

// Environment knobs. They describe host facts rather than timings, so they stay
// out of the YAML config and the settings UI; set them with a systemd drop-in on
// linuxio-monitoring.service. This is the complete list: nothing else in the
// daemon reads the environment.
const (
	// EnvNICs filters the network interfaces that are reported.
	EnvNICs = "NICS"

	// EnvSensors restricts the temperature sensors that are reported: a
	// comma-separated allowlist, a "-" prefixed denylist, or an empty value to
	// skip temperature collection. EnvSysSensors points the sensor reads at
	// another /sys, EnvPrimarySensor names the sensor that feeds the dashboard
	// temperature and EnvSensorsTimeout bounds one read (a Go duration).
	EnvSensors        = "SENSORS"
	EnvSysSensors     = "SYS_SENSORS"
	EnvPrimarySensor  = "PRIMARY_SENSOR"
	EnvSensorsTimeout = "SENSORS_TIMEOUT"

	// EnvGPUCollector forces one GPU collector, EnvSkipGPU ("true") disables GPU
	// monitoring and EnvIntelGPUDevice selects the device intel_gpu_top reads.
	EnvGPUCollector   = "GPU_COLLECTOR"
	EnvSkipGPU        = "SKIP_GPU"
	EnvIntelGPUDevice = "INTEL_GPU_DEVICE"

	// EnvSmartDevices lists the devices smartctl scans (split on
	// EnvSmartDevicesSeparator, default ","); EnvExcludeSmart lists devices to
	// skip.
	EnvSmartDevices          = "SMART_DEVICES"
	EnvSmartDevicesSeparator = "SMART_DEVICES_SEPARATOR"
	EnvExcludeSmart          = "EXCLUDE_SMART"

	// EnvFilesystem names the device or mountpoint of the root filesystem's
	// disk and EnvExtraFilesystems lists further filesystems to track.
	EnvFilesystem       = "FILESYSTEM"
	EnvExtraFilesystems = "EXTRA_FILESYSTEMS"

	// EnvMemCalc selects the memory-used formula; "htop" subtracts buffers and
	// cache the way htop does.
	EnvMemCalc = "MEM_CALC"

	// EnvDockerHost overrides the Docker or Podman endpoint, EnvDockerTimeout
	// bounds one API call (a Go duration) and EnvExcludeContainers lists
	// comma-separated container name patterns to skip.
	EnvDockerHost        = "DOCKER_HOST"
	EnvDockerTimeout     = "DOCKER_TIMEOUT"
	EnvExcludeContainers = "EXCLUDE_CONTAINERS"

	// EnvHTTPLog turns on per-request logging at debug level; EnvRequestLog is
	// its older spelling and is read only when EnvHTTPLog is unset.
	EnvHTTPLog    = "HTTP_LOG"
	EnvRequestLog = "REQUEST_LOG"
)

// LookupEnv returns the value of an environment knob and whether it is set.
func LookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}
