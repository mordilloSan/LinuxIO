package system

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
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

	groups := FetchSensorsInfo()
	require.Len(t, groups, 2)

	require.Equal(t, "coretemp-isa-0000", groups[0].Adapter)
	require.Equal(t, []SensorReading{
		{Label: "Core 0 (input)", Value: 48.0, Kind: SensorReadingKindNumber, Unit: "°C", field: "temp2_input"},
		{Label: "Core 0 (max)", Value: 95.0, Kind: SensorReadingKindNumber, Unit: "°C", field: "temp2_max"},
		{Label: "Core 0 (crit)", Value: 100.0, Kind: SensorReadingKindNumber, Unit: "°C", field: "temp2_crit"},
		{Label: "Core 0 (alarm)", Value: false, Kind: SensorReadingKindBoolean, Unit: "", field: "temp2_alarm"},
		{Label: "Package id 0 (input)", Value: 55.5, Kind: SensorReadingKindNumber, Unit: "°C", field: "temp1_input"},
		{Label: "Package id 0 (max)", Value: 95.0, Kind: SensorReadingKindNumber, Unit: "°C", field: "temp1_max"},
		{Label: "Package id 0 (crit)", Value: 100.0, Kind: SensorReadingKindNumber, Unit: "°C", field: "temp1_crit"},
		{Label: "Package id 0 (alarm)", Value: true, Kind: SensorReadingKindBoolean, Unit: "", field: "temp1_alarm"},
	}, groups[0].Readings)

	require.Equal(t, "nct6798-isa-0290", groups[1].Adapter)
	require.Equal(t, []SensorReading{
		{Label: "3VCC (input)", Value: 3.31, Kind: SensorReadingKindNumber, Unit: "V", field: "in0_input"},
		{Label: "3VCC (min)", Value: 3.14, Kind: SensorReadingKindNumber, Unit: "V", field: "in0_min"},
		{Label: "3VCC (max)", Value: 3.47, Kind: SensorReadingKindNumber, Unit: "V", field: "in0_max"},
		{Label: "fan1 (input)", Value: 1520.0, Kind: SensorReadingKindNumber, Unit: "RPM", field: "fan1_input"},
		{Label: "fan1 (alarm)", Value: false, Kind: SensorReadingKindBoolean, Unit: "", field: "fan1_alarm"},
		{Label: "intrusion0", Value: true, Kind: SensorReadingKindBoolean, Unit: "", field: "alarm"},
		{Label: "nested (subsystem / input)", Value: 65.2, Kind: SensorReadingKindNumber, Unit: "W", field: "power1_input"},
		{Label: "nested (subsystem / alarm)", Value: false, Kind: SensorReadingKindBoolean, Unit: "", field: "power1_alarm"},
	}, groups[1].Readings)
}

func TestFetchSensorsInfoReturnsNilOnMalformedJSON(t *testing.T) {
	stubSensorsCommand(t, "malformed")

	require.Nil(t, FetchSensorsInfo())
}

func TestFetchSensorsInfoReturnsNilOnCommandFailure(t *testing.T) {
	stubSensorsCommand(t, "fail")

	require.Nil(t, FetchSensorsInfo())
}

func TestGetTemperatureMapUsesInputReadingsOnly(t *testing.T) {
	stubSensorsCommand(t, "success")

	temps := getTemperatureMap()
	require.Equal(t, map[string]float64{
		"core0":   48.0,
		"package": 55.5,
	}, temps)
}

func stubSensorsCommand(t *testing.T, mode string) {
	original := sensorsCommand
	sensorsCommand = func(name string, args ...string) *exec.Cmd {
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
