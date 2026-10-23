// Package sensors collects temperature readings through gopsutil, honouring the
// SENSORS allow/deny list and the dashboard (primary) sensor.
package sensors

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

	psutilCommon "github.com/shirou/gopsutil/v4/common"
	psutilSensors "github.com/shirou/gopsutil/v4/sensors"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

var errTemperatureFetchTimeout = errors.New("temperature collection timed out")

// Matches sensors.TemperaturesWithContext to allow for panic recovery (gopsutil/issues/1832)
type getTempsFn func(ctx context.Context) ([]psutilSensors.TemperatureStat, error)

// Config holds the sensor selection, timeout and read gate.
type Config struct {
	envMap         psutilCommon.EnvMap
	sensors        map[string]struct{}
	primarySensor  string
	timeout        time.Duration
	readSem        chan struct{}
	isBlacklist    bool
	hasWildcards   bool
	skipCollection bool
	firstRun       bool
}

// NewConfig reads the sensor selection from the environment.
func NewConfig() *Config {
	primarySensor, _ := config.LookupEnv(config.EnvPrimarySensor)
	sysSensors, _ := config.LookupEnv(config.EnvSysSensors)
	sensorsEnvVal, sensorsSet := config.LookupEnv(config.EnvSensors)
	skipCollection := sensorsSet && sensorsEnvVal == ""
	sensorsTimeout, _ := config.LookupEnv(config.EnvSensorsTimeout)

	return newConfigWithEnv(primarySensor, sysSensors, sensorsEnvVal, sensorsTimeout, skipCollection)
}

// newConfigWithEnv creates a Config with the provided environment variables.
// skipCollection is set when SENSORS was explicitly set, even to an empty string.
func newConfigWithEnv(primarySensor, sysSensors, sensorsEnvVal, sensorsTimeout string, skipCollection bool) *Config {
	timeout := 2 * time.Second
	if sensorsTimeout != "" {
		if d, err := time.ParseDuration(sensorsTimeout); err == nil {
			timeout = d
		} else {
			slog.Warn("Invalid SENSORS_TIMEOUT", "value", sensorsTimeout)
		}
	}

	cfg := &Config{
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
		cfg.envMap = psutilCommon.EnvMap{psutilCommon.HostSysEnvKey: sysSensors}
	}

	// handle blacklist
	if strings.HasPrefix(sensorsEnvVal, "-") {
		cfg.isBlacklist = true
		sensorsEnvVal = sensorsEnvVal[1:]
	}

	for sensor := range strings.SplitSeq(sensorsEnvVal, ",") {
		sensor = strings.TrimSpace(sensor)
		if sensor != "" {
			cfg.sensors[sensor] = struct{}{}
			if strings.Contains(sensor, "*") {
				cfg.hasWildcards = true
			}
		}
	}

	return cfg
}

// PrimarySensor names the sensor that feeds the dashboard temperature, or "".
func (c *Config) PrimarySensor() string { return c.primarySensor }

// Timeout bounds one sensor read.
func (c *Config) Timeout() time.Duration { return c.timeout }

// Update refreshes the stats' temperatures and the dashboard temperature in
// info from the latest sensor readings.
//
//nolint:gocognit // Temperature collection merges heterogeneous sensor sources and fallback paths.
func (c *Config) Update(ctx context.Context, systemStats *systemmodel.Stats, info *systemmodel.Info) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// skip if sensors whitelist is set to empty string
	if c.skipCollection {
		slog.Debug("Skipping temperature collection")
		return nil
	}

	// reset high temp
	info.DashboardTemp = 0

	temps, err := c.getTempsWithTimeout(ctx, getSensorTemps)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// retry once on panic (gopsutil/issues/1832)
		if !errors.Is(err, errTemperatureFetchTimeout) {
			temps, err = c.getTempsWithTimeout(ctx, getSensorTemps)
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
		if !isValidSensor(sensorName, c) {
			continue
		}
		// set dashboard temperature
		switch c.primarySensor {
		case "":
			info.DashboardTemp = max(info.DashboardTemp, sensor.Temperature)
		case sensorName:
			info.DashboardTemp = sensor.Temperature
		}
		systemStats.Temperatures[sensorName] = units.TwoDecimals(sensor.Temperature)
	}
	return nil
}

// getTempsWithPanicRecovery wraps sensors.TemperaturesWithContext to recover from panics (gopsutil/issues/1832)
func (c *Config) getTempsWithPanicRecovery(ctx context.Context, getTemps getTempsFn) (temps []psutilSensors.TemperatureStat, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	// get sensor data (error ignored intentionally as it may be only with one sensor)
	temps, _ = getTemps(ctx)
	return temps, nil
}

func (c *Config) tryStartRead() bool {
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

func (c *Config) finishRead() {
	if c.readSem == nil {
		return
	}
	select {
	case <-c.readSem:
	default:
	}
}

func (c *Config) getTempsWithTimeout(ctx context.Context, getTemps getTempsFn) ([]psutilSensors.TemperatureStat, error) {
	type result struct {
		temps []psutilSensors.TemperatureStat
		err   error
	}

	// Use a longer timeout on the first run to allow for initialization
	// (e.g. slow sensor subsystem startup)
	timeout := c.timeout
	if c.firstRun {
		c.firstRun = false
		timeout = 10 * time.Second
	}

	if !c.tryStartRead() {
		return nil, errTemperatureFetchTimeout
	}

	// Derive a timeout-aware context so a context-aware gopsutil call cancels
	// when the timeout fires. The worker goroutine still survives the parent's
	// return if gopsutil ignores ctx, but this gate prevents one stuck read from
	// growing into one new goroutine per poll.
	parentCtx := ctx
	if len(c.envMap) > 0 {
		ctx = context.WithValue(ctx, psutilCommon.EnvKey, c.envMap)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultCh := make(chan result, 1)
	go func() {
		defer c.finishRead()
		// This goroutine can outlive the timeout below, so label it: a hung
		// sensor read is then obvious in a traceback or leak profile.
		pprof.Do(ctx, pprof.Labels("component", "sensors"), func(ctx context.Context) {
			temps, err := c.getTempsWithPanicRecovery(ctx, getTemps)
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
func isValidSensor(sensorName string, cfg *Config) bool {
	// if no sensors configured, everything is valid
	if len(cfg.sensors) == 0 {
		return true
	}

	// Exact match - return true if whitelist, false if blacklist
	if _, exactMatch := cfg.sensors[sensorName]; exactMatch {
		return !cfg.isBlacklist
	}

	// If no wildcards, return true if blacklist, false if whitelist
	if !cfg.hasWildcards {
		return cfg.isBlacklist
	}

	// Check for wildcard patterns
	for pattern := range cfg.sensors {
		if !strings.Contains(pattern, "*") {
			continue
		}
		if match, _ := path.Match(pattern, sensorName); match {
			return !cfg.isBlacklist
		}
	}

	return cfg.isBlacklist
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
