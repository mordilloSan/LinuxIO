package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/common"
	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

var errTemperatureFetchTimeout = errors.New("temperature collection timed out")

// Matches sensors.TemperaturesWithContext to allow for panic recovery (gopsutil/issues/1832)
type getTempsFn func(ctx context.Context) ([]sensors.TemperatureStat, error)

type SensorConfig struct {
	envMap         common.EnvMap
	sensors        map[string]struct{}
	primarySensor  string
	timeout        time.Duration
	readSem        chan struct{}
	isBlacklist    bool
	hasWildcards   bool
	skipCollection bool
	firstRun       bool
}

func (a *App) newSensorConfig() *SensorConfig {
	primarySensor, _ := utils.GetEnv("PRIMARY_SENSOR")
	sysSensors, _ := utils.GetEnv("SYS_SENSORS")
	sensorsEnvVal, sensorsSet := utils.GetEnv("SENSORS")
	skipCollection := sensorsSet && sensorsEnvVal == ""
	sensorsTimeout, _ := utils.GetEnv("SENSORS_TIMEOUT")

	return a.newSensorConfigWithEnv(primarySensor, sysSensors, sensorsEnvVal, sensorsTimeout, skipCollection)
}

// newSensorConfigWithEnv creates a SensorConfig with the provided environment variables
// sensorsSet indicates if the SENSORS environment variable was explicitly set (even to empty string)
func (a *App) newSensorConfigWithEnv(primarySensor, sysSensors, sensorsEnvVal, sensorsTimeout string, skipCollection bool) *SensorConfig {
	timeout := 2 * time.Second
	if sensorsTimeout != "" {
		if d, err := time.ParseDuration(sensorsTimeout); err == nil {
			timeout = d
		} else {
			slog.Warn("Invalid SENSORS_TIMEOUT", "value", sensorsTimeout)
		}
	}

	config := &SensorConfig{
		primarySensor:  primarySensor,
		timeout:        timeout,
		readSem:        make(chan struct{}, 1),
		skipCollection: skipCollection,
		firstRun:       true,
		sensors:        make(map[string]struct{}),
	}

	// Set sensors context (allows overriding sys location for sensors)
	if sysSensors != "" {
		slog.Info("SYS_SENSORS", "path", sysSensors)
		config.envMap = common.EnvMap{common.HostSysEnvKey: sysSensors}
	}

	// handle blacklist
	if strings.HasPrefix(sensorsEnvVal, "-") {
		config.isBlacklist = true
		sensorsEnvVal = sensorsEnvVal[1:]
	}

	for sensor := range strings.SplitSeq(sensorsEnvVal, ",") {
		sensor = strings.TrimSpace(sensor)
		if sensor != "" {
			config.sensors[sensor] = struct{}{}
			if strings.Contains(sensor, "*") {
				config.hasWildcards = true
			}
		}
	}

	return config
}

// updateTemperatures updates the agent with the latest sensor temperatures
//
//nolint:gocognit // Temperature collection merges heterogeneous sensor sources and fallback paths.
func (a *App) updateTemperatures(ctx context.Context, systemStats *system.Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// skip if sensors whitelist is set to empty string
	if a.sensorConfig.skipCollection {
		slog.Debug("Skipping temperature collection")
		return nil
	}

	// reset high temp
	a.systemInfoManager.systemInfo.DashboardTemp = 0

	temps, err := a.getTempsWithTimeout(ctx, getSensorTemps)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// retry once on panic (gopsutil/issues/1832)
		if !errors.Is(err, errTemperatureFetchTimeout) {
			temps, err = a.getTempsWithTimeout(ctx, getSensorTemps)
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			slog.Warn("Error updating temperatures", "err", err)
			if len(systemStats.Temperatures) > 0 {
				systemStats.Temperatures = make(map[string]float64)
			}
			return nil
		}
	}
	slog.Debug("Temperature sensors discovered", "count", len(temps))

	// return if no sensors
	if len(temps) == 0 {
		return nil
	}

	systemStats.Temperatures = make(map[string]float64, len(temps))
	for i, sensor := range temps {
		// scale temperature
		if sensor.Temperature != 0 && sensor.Temperature < 1 {
			sensor.Temperature = scaleTemperature(sensor.Temperature)
		}
		// skip if temperature is unreasonable
		if sensor.Temperature <= 0 || sensor.Temperature >= 200 {
			continue
		}
		sensorName := sensor.SensorKey
		if _, ok := systemStats.Temperatures[sensorName]; ok {
			// if key already exists, append int to key
			sensorName = sensorName + "_" + strconv.Itoa(i)
		}
		// skip if not in whitelist or blacklist
		if !isValidSensor(sensorName, a.sensorConfig) {
			continue
		}
		// set dashboard temperature
		switch a.sensorConfig.primarySensor {
		case "":
			a.systemInfoManager.systemInfo.DashboardTemp = max(a.systemInfoManager.systemInfo.DashboardTemp, sensor.Temperature)
		case sensorName:
			a.systemInfoManager.systemInfo.DashboardTemp = sensor.Temperature
		}
		systemStats.Temperatures[sensorName] = utils.TwoDecimals(sensor.Temperature)
	}
	return nil
}

// getTempsWithPanicRecovery wraps sensors.TemperaturesWithContext to recover from panics (gopsutil/issues/1832)
func (a *App) getTempsWithPanicRecovery(ctx context.Context, getTemps getTempsFn) (temps []sensors.TemperatureStat, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	// get sensor data (error ignored intentionally as it may be only with one sensor)
	temps, _ = getTemps(ctx)
	return
}

func (c *SensorConfig) tryStartRead() bool {
	if c.readSem == nil {
		c.readSem = make(chan struct{}, 1)
	}
	select {
	case c.readSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *SensorConfig) finishRead() {
	if c.readSem == nil {
		return
	}
	select {
	case <-c.readSem:
	default:
	}
}

func (a *App) getTempsWithTimeout(ctx context.Context, getTemps getTempsFn) ([]sensors.TemperatureStat, error) {
	type result struct {
		temps []sensors.TemperatureStat
		err   error
	}

	// Use a longer timeout on the first run to allow for initialization
	// (e.g. slow sensor subsystem startup)
	timeout := a.sensorConfig.timeout
	if a.sensorConfig.firstRun {
		a.sensorConfig.firstRun = false
		timeout = 10 * time.Second
	}

	if !a.sensorConfig.tryStartRead() {
		return nil, errTemperatureFetchTimeout
	}

	// Derive a timeout-aware context so a context-aware gopsutil call cancels
	// when the timeout fires. The worker goroutine still survives the parent's
	// return if gopsutil ignores ctx, but this gate prevents one stuck read from
	// growing into one new goroutine per poll.
	parentCtx := ctx
	if len(a.sensorConfig.envMap) > 0 {
		ctx = context.WithValue(ctx, common.EnvKey, a.sensorConfig.envMap)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultCh := make(chan result, 1)
	go func() {
		defer a.sensorConfig.finishRead()
		// This goroutine can outlive the timeout below, so label it: a hung
		// sensor read is then obvious in a traceback or leak profile.
		pprof.Do(ctx, pprof.Labels("component", "sensors"), func(ctx context.Context) {
			temps, err := a.getTempsWithPanicRecovery(ctx, getTemps)
			select {
			case resultCh <- result{temps: temps, err: err}:
			case <-ctx.Done():
			}
		})
	}()

	select {
	case res := <-resultCh:
		return res.temps, res.err
	case <-ctx.Done():
		if err := parentCtx.Err(); err != nil {
			return nil, err
		}
		return nil, errTemperatureFetchTimeout
	}
}

// isValidSensor checks if a sensor is valid based on the sensor name and the sensor config
func isValidSensor(sensorName string, config *SensorConfig) bool {
	// if no sensors configured, everything is valid
	if len(config.sensors) == 0 {
		return true
	}

	// Exact match - return true if whitelist, false if blacklist
	if _, exactMatch := config.sensors[sensorName]; exactMatch {
		return !config.isBlacklist
	}

	// If no wildcards, return true if blacklist, false if whitelist
	if !config.hasWildcards {
		return config.isBlacklist
	}

	// Check for wildcard patterns
	for pattern := range config.sensors {
		if !strings.Contains(pattern, "*") {
			continue
		}
		if match, _ := path.Match(pattern, sensorName); match {
			return !config.isBlacklist
		}
	}

	return config.isBlacklist
}

// scaleTemperature scales temperatures in fractional values to reasonable Celsius values
func scaleTemperature(temp float64) float64 {
	if temp > 1 {
		return temp
	}
	scaled100 := temp * 100
	scaled1000 := temp * 1000

	if scaled100 >= 15 && scaled100 <= 95 {
		return scaled100
	} else if scaled1000 >= 15 && scaled1000 <= 95 {
		return scaled1000
	}
	return scaled100
}
