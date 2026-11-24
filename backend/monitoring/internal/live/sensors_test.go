package live

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const sensorsJSONFixture = `{
  "coretemp-isa-0000": {
    "Adapter": "ISA adapter",
    "Core 0": {
      "temp2_input": 48,
      "temp2_max": 95,
      "temp2_crit": 100,
      "temp2_alarm": false
    },
    "Package id 0": {
      "temp1_input": 55.5,
      "temp1_max": 95,
      "temp1_crit": 100,
      "temp1_alarm": true
    }
  },
  "nct6798-isa-0290": {
    "Adapter": "ISA adapter",
    "3VCC": {
      "in0_input": 3.31,
      "in0_min": 3.14,
      "in0_max": 3.47
    },
    "fan1": {
      "fan1_input": 1520,
      "fan1_alarm": false
    },
    "intrusion0": {
      "alarm": true
    },
    "nested": {
      "subsystem": {
        "power1_input": 65.2,
        "power1_alarm": false
      }
    }
  }
}`

func TestFetchSensorsInfoParsesJSONReadings(t *testing.T) {
	stubSensorsCommand(t, "success")

	groups := fetchSensorsInfo(context.Background())
	require.Len(t, groups, 2)

	require.Equal(t, "coretemp-isa-0000", groups[0].Adapter)
	require.Equal(t, []monitoringapi.SensorReading{
		{Label: "Core 0 (input)", Value: 48.0, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp2_input"},
		{Label: "Core 0 (max)", Value: 95.0, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp2_max"},
		{Label: "Core 0 (crit)", Value: 100.0, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp2_crit"},
		{Label: "Core 0 (alarm)", Value: 0, Kind: monitoringapi.SensorReadingKindBoolean, Unit: "", Field: "temp2_alarm"},
		{Label: "Package id 0 (input)", Value: 55.5, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp1_input"},
		{Label: "Package id 0 (max)", Value: 95.0, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp1_max"},
		{Label: "Package id 0 (crit)", Value: 100.0, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp1_crit"},
		{Label: "Package id 0 (alarm)", Value: 1, Kind: monitoringapi.SensorReadingKindBoolean, Unit: "", Field: "temp1_alarm"},
	}, groups[0].Readings)

	require.Equal(t, "nct6798-isa-0290", groups[1].Adapter)
	require.Equal(t, []monitoringapi.SensorReading{
		{Label: "3VCC (input)", Value: 3.31, Kind: monitoringapi.SensorReadingKindNumber, Unit: "V", Field: "in0_input"},
		{Label: "3VCC (min)", Value: 3.14, Kind: monitoringapi.SensorReadingKindNumber, Unit: "V", Field: "in0_min"},
		{Label: "3VCC (max)", Value: 3.47, Kind: monitoringapi.SensorReadingKindNumber, Unit: "V", Field: "in0_max"},
		{Label: "fan1 (input)", Value: 1520.0, Kind: monitoringapi.SensorReadingKindNumber, Unit: "RPM", Field: "fan1_input"},
		{Label: "fan1 (alarm)", Value: 0, Kind: monitoringapi.SensorReadingKindBoolean, Unit: "", Field: "fan1_alarm"},
		{Label: "intrusion0", Value: 1, Kind: monitoringapi.SensorReadingKindBoolean, Unit: "", Field: "alarm"},
		{Label: "nested (subsystem / input)", Value: 65.2, Kind: monitoringapi.SensorReadingKindNumber, Unit: "W", Field: "power1_input"},
		{Label: "nested (subsystem / alarm)", Value: 0, Kind: monitoringapi.SensorReadingKindBoolean, Unit: "", Field: "power1_alarm"},
	}, groups[1].Readings)
}

func TestFetchSensorsInfoReturnsNilOnMalformedJSON(t *testing.T) {
	stubSensorsCommand(t, "malformed")

	require.Nil(t, fetchSensorsInfo(context.Background()))
}

func TestFetchSensorsInfoToleratesDuplicateKeysAndInvalidUTF8(t *testing.T) {
	stubSensorsCommand(t, "duplicate")

	groups := fetchSensorsInfo(context.Background())
	require.Len(t, groups, 1)
	require.Equal(t, "dell_smm-virtual-0", groups[0].Adapter)
}

func TestFetchSensorsInfoReturnsNilOnCommandFailure(t *testing.T) {
	stubSensorsCommand(t, "fail")

	require.Nil(t, fetchSensorsInfo(context.Background()))
}

func TestGetTemperatureMapUsesInputReadingsOnly(t *testing.T) {
	stubSensorsCommand(t, "success")

	temps := CPUTemperatures(fetchSensorsInfo(context.Background()))
	require.Equal(t, map[string]float64{
		"core0":   48.0,
		"package": 55.5,
	}, temps)
}

// Two sockets each report "Package id" and "Core 0"; neither overwrites the other.
func TestCPUTemperaturesNamesEachChipOnAMultiSocketHost(t *testing.T) {
	reading := func(label string, value float64) monitoringapi.SensorReading {
		return monitoringapi.SensorReading{Label: label, Field: "input", Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Value: value}
	}
	groups := []monitoringapi.SensorGroup{
		{Adapter: "coretemp-isa-0000", Readings: []monitoringapi.SensorReading{reading("Package id 0", 50), reading("Core 0", 45), reading("Core 1", 46)}},
		{Adapter: "nct6798-isa-0290", Readings: []monitoringapi.SensorReading{reading("SYSTIN", 30)}},
		{Adapter: "coretemp-isa-0001", Readings: []monitoringapi.SensorReading{reading("Package id 1", 60), reading("Core 0", 55)}},
	}
	require.Equal(t, map[string]float64{
		"package0": 50, "package0core0": 45, "package0core1": 46,
		"package1": 60, "package1core0": 55,
	}, CPUTemperatures(groups))

	// AMD reports one Tctl per socket and no cores.
	groups = []monitoringapi.SensorGroup{
		{Adapter: "k10temp-pci-00c3", Readings: []monitoringapi.SensorReading{reading("Tctl", 40)}},
		{Adapter: "k10temp-pci-00cb", Readings: []monitoringapi.SensorReading{reading("Tctl", 42)}},
	}
	require.Equal(t, map[string]float64{"package0": 40, "package1": 42}, CPUTemperatures(groups))
}

func TestInputReadingsKeepsMeasurementsOnly(t *testing.T) {
	stubSensorsCommand(t, "success")

	require.Equal(t, map[string]map[string]float64{
		"coretemp-isa-0000": {"Core 0 (input)": 48, "Package id 0 (input)": 55.5},
		"nct6798-isa-0290":  {"3VCC (input)": 3.31, "fan1 (input)": 1520, "nested (subsystem / input)": 65.2},
	}, InputReadings(fetchSensorsInfo(context.Background())))
}

func stubSensorsCommand(t *testing.T, mode string) {
	original := sensorsCommand
	sensorsCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperSensorsCommand", "--", mode)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	t.Cleanup(func() {
		sensorsCommand = original
	})
}

func TestHelperSensorsCommand(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "success":
		fmt.Fprint(os.Stdout, sensorsJSONFixture)
		os.Exit(0)
	case "duplicate":
		fmt.Fprint(os.Stdout, "{\"dell_smm-virtual-0\": {\"Adapter\": \"Virtual device\", \"Other\": {\"temp1_input\": 40}, \"Other\": {\"temp1_input\": 41}, \"bad\xff\": {\"fan1_input\": 1000}}}")
		os.Exit(0)
	case "malformed":
		fmt.Fprint(os.Stdout, `{"broken":`)
		os.Exit(0)
	case "fail":
		fmt.Fprint(os.Stderr, "sensors failed")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q", mode)
		os.Exit(2)
	}
}
