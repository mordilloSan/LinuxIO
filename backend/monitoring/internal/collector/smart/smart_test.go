package smart

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

func readSmartFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "smart", name))
	require.NoError(t, err)
	return data
}

// parseSmart runs parseSmartctl and requires it to succeed.
func parseSmart(t *testing.T, output []byte, dtype string) *smartmodel.SmartData {
	t.Helper()
	data, _, err := parseSmartctl(output, dtype)
	require.NoError(t, err)
	return data
}

func assertAttrValue(t *testing.T, attributes []*smartmodel.SmartAttribute, name string, expected uint64) {
	t.Helper()
	attr := findAttr(attributes, name)
	require.NotNil(t, attr, "expected attribute %s to be present", name)
	assert.Equal(t, expected, attr.RawValue, "attribute %s", name)
}

func findAttr(attributes []*smartmodel.SmartAttribute, name string) *smartmodel.SmartAttribute {
	for _, attr := range attributes {
		if attr != nil && attr.Name == name {
			return attr
		}
	}
	return nil
}

func TestParseSmartctlScsi(t *testing.T) {
	deviceData := parseSmart(t, readSmartFixture(t, "scsi.json"), "")

	assert.Equal(t, "YADRO WUH721414AL4204", deviceData.ModelName)
	assert.Equal(t, "9YHSDH9B", deviceData.SerialNumber)
	assert.Equal(t, "C240", deviceData.FirmwareVersion)
	assert.Equal(t, "/dev/sde", deviceData.DiskName)
	assert.Equal(t, "scsi", deviceData.DiskType)
	assert.EqualValues(t, 34, deviceData.Temperature)
	assert.Equal(t, "PASSED", deviceData.SmartStatus)
	assert.EqualValues(t, 14000519643136, deviceData.Capacity)

	assertAttrValue(t, deviceData.Attributes, "PowerOnHours", 458)
	assertAttrValue(t, deviceData.Attributes, "PowerOnMinutes", 25)
	assertAttrValue(t, deviceData.Attributes, "GrownDefectList", 0)
	assertAttrValue(t, deviceData.Attributes, "StartStopCycles", 2)
	assertAttrValue(t, deviceData.Attributes, "LoadUnloadCycles", 418)
	assertAttrValue(t, deviceData.Attributes, "ReadGigabytesProcessed", 3641)
	assertAttrValue(t, deviceData.Attributes, "WriteGigabytesProcessed", 2124590)
	assertAttrValue(t, deviceData.Attributes, "VerifyGigabytesProcessed", 0)
}

func TestParseSmartctlSata(t *testing.T) {
	deviceData, protocol, err := parseSmartctl(readSmartFixture(t, "sda.json"), "")
	require.NoError(t, err)
	assert.Equal(t, "ata", protocol)

	assert.Equal(t, "9C40918040082", deviceData.SerialNumber)
	assert.Equal(t, "P3-2TB", deviceData.ModelName)
	assert.Equal(t, "X0104A0", deviceData.FirmwareVersion)
	assert.Equal(t, "/dev/sda", deviceData.DiskName)
	assert.Equal(t, "sat", deviceData.DiskType)
	assert.Equal(t, uint8(31), deviceData.Temperature)
	assert.Equal(t, "PASSED", deviceData.SmartStatus)
	assert.Equal(t, uint64(2048408248320), deviceData.Capacity)
	assertAttrValue(t, deviceData.Attributes, "Temperature_Celsius", 31)
}

func TestParseSmartctlNvme(t *testing.T) {
	deviceData := parseSmart(t, readSmartFixture(t, "nvme0.json"), "")

	assert.Equal(t, "2024031600129", deviceData.SerialNumber)
	assert.Equal(t, "PELADN 512GB", deviceData.ModelName)
	assert.Equal(t, "VC2S038E", deviceData.FirmwareVersion)
	assert.Equal(t, "/dev/nvme0", deviceData.DiskName)
	assert.Equal(t, "nvme", deviceData.DiskType)
	assert.Equal(t, uint8(61), deviceData.Temperature)
	assert.Equal(t, "PASSED", deviceData.SmartStatus)
	assert.Equal(t, uint64(512110190592), deviceData.Capacity)
	assertAttrValue(t, deviceData.Attributes, "PercentageUsed", 0)
	assertAttrValue(t, deviceData.Attributes, "DataUnitsWritten", 16040567)
}

// The storage page reads the typed self-test history, the NVMe health details
// and the power counters, so the parser keeps them in the record.
func TestParseSmartctlKeepsLiveHealthAndSelfTests(t *testing.T) {
	ata := parseSmart(t, readSmartFixture(t, "sda.json"), "")
	require.NotNil(t, ata.Device)
	assert.Equal(t, "ATA", ata.Device.Protocol)
	require.NotNil(t, ata.AtaSmartSelfTestLog)
	require.NotNil(t, ata.AtaSmartSelfTestLog.Standard)
	assert.Len(t, ata.AtaSmartSelfTestLog.Standard.Table, 3)
	require.NotNil(t, ata.PowerOnTime)
	assert.Equal(t, uint64(7344), ata.PowerOnTime.Hours)
	require.NotNil(t, ata.PowerCycleCount)
	assert.Equal(t, uint64(104), *ata.PowerCycleCount)

	nvme := parseSmart(t, readSmartFixture(t, "nvme0.json"), "")
	require.NotNil(t, nvme.NVMeSmartHealthInformationLog)
	health := nvme.NVMeSmartHealthInformationLog
	assert.Equal(t, uint64(4399), health.PowerOnHours)
	assert.Equal(t, uint64(16040567), health.DataUnitsWritten)
	assert.Equal(t, int64(61), health.Temperature)
	assert.Equal(t, int64(-1), health.NSID)
	assert.Equal(t, []int64{61, 44}, health.TemperatureSensors)
	require.NotNil(t, nvme.NVMeSelfTestLog)
	require.NotNil(t, nvme.NVMeVersion)
	require.NotNil(t, nvme.PowerOnTime)
	assert.Equal(t, uint64(4399), nvme.PowerOnTime.Hours)
	require.NotNil(t, nvme.PowerCycleCount)
	assert.Equal(t, uint64(430), *nvme.PowerCycleCount)
}

// The -d type used for the query is reported as the disk type, so RAID and
// bridge types stay visible.
func TestParseSmartctlReportsQueryType(t *testing.T) {
	assert.Equal(t, "sat+megaraid", parseSmart(t, readSmartFixture(t, "sda.json"), "sat+megaraid").DiskType)
	assert.Equal(t, "ata", parseSmart(t, readSmartFixture(t, "sda.json"), "ata").DiskType)
}

// The status comes from smartctl's health field alone: a missing one is
// UNKNOWN, never FAILED, whatever the temperature.
func TestParseSmartctlStatus(t *testing.T) {
	for _, test := range []struct {
		name, status string
		want         string
	}{
		{"passed", `,"smart_status":{"passed":true}`, "PASSED"},
		{"failed", `,"smart_status":{"passed":false}`, "FAILED"},
		{"missing", "", "UNKNOWN"},
		{"null", `,"smart_status":{"passed":null}`, "UNKNOWN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := fmt.Sprintf(`{
				"device": {"name": "/dev/sda", "type": "sat", "protocol": "ATA"},
				"serial_number": "STATUS",
				"temperature": {"current": 30},
				"ata_smart_attributes": {"table": [{"id": 197, "raw": {"value": 1, "string": "1"}}]}
				%s
			}`, test.status)
			assert.Equal(t, test.want, parseSmart(t, []byte(output), "").SmartStatus)
		})
	}
}

// Output that identifies a disk but holds no health data for its protocol is
// rejected so the next attempt runs, instead of storing zeros.
func TestParseSmartctlRequiresHealthData(t *testing.T) {
	for _, test := range []struct {
		name, protocol, health string
		valid                  bool
	}{
		{"scsi failed status", "SCSI", `,"smart_status":{"passed":false}`, true},
		{"scsi passed status", "SCSI", `,"smart_status":{"passed":true}`, true},
		{"scsi error log", "SCSI", `,"scsi_error_counter_log":{"read":{}}`, true},
		{"scsi null status", "SCSI", `,"smart_status":null`, false},
		{"scsi nothing", "SCSI", "", false},
		{"nvme zero log", "NVMe", `,"nvme_smart_health_information_log":{"temperature":0}`, true},
		{"nvme null log", "NVMe", `,"nvme_smart_health_information_log":null`, false},
		{"nvme nothing", "NVMe", "", false},
		{"ata attributes", "ATA", `,"ata_smart_attributes":{"table":[{"id":5}]}`, true},
		{"ata status only", "ATA", `,"smart_status":{"passed":true}`, true},
		{"ata devstat only", "ATA", `,"ata_device_statistics":{"pages":[]}`, true},
		{"ata empty table", "ATA", `,"ata_smart_attributes":{"table":[]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := fmt.Sprintf(`{"device":{"name":"/dev/sda","protocol":%q},"serial_number":"HEALTH"%s}`, test.protocol, test.health)
			_, _, err := parseSmartctl([]byte(output), "")
			if test.valid {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, errNoHealthData)
			}
		})
	}

	_, _, err := parseSmartctl(readSmartFixture(t, "scsi_identity.json"), "")
	require.ErrorIs(t, err, errNoHealthData)

	// An NVMe controller can return identity fields but fail the health log.
	_, _, err = parseSmartctl([]byte(`{
		"smartctl": {"exit_status": 2},
		"device": {"name": "/dev/nvme0", "type": "nvme"},
		"model_name": "Netac NVMe SSD 500GB",
		"serial_number": "IDENTITY-ONLY",
		"user_capacity": {"bytes": 500107862016}
	}`), "")
	require.ErrorIs(t, err, errNoHealthData)

	// Real smartctl output without the permission to read the NVMe health log.
	_, _, err = parseSmartctl(readSmartFixture(t, "nvme_permission_denied.json"), "nvme")
	assert.ErrorIs(t, err, errNoHealthData)
}

func TestParseSmartctlRejectsInvalidOutput(t *testing.T) {
	for name, output := range map[string]string{
		"not json":  "not json",
		"no serial": `{"device":{"name":"/dev/sda"},"smart_status":{"passed":true}}`,
		"virtual":   `{"serial_number":"V1","model_name":"QEMU HARDDISK","smart_status":{"passed":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseSmartctl([]byte(output), "")
			assert.Error(t, err)
		})
	}
}

// Without device.protocol the protocol is inferred from the sections present.
func TestSmartProtocolInference(t *testing.T) {
	for _, test := range []struct {
		data smartmodel.SmartctlOutput
		want string
	}{
		{smartmodel.SmartctlOutput{Device: smartmodel.DeviceInfo{Protocol: "NVMe"}}, "nvme"},
		{smartmodel.SmartctlOutput{Device: smartmodel.DeviceInfo{Type: "nvme"}}, "nvme"},
		{smartmodel.SmartctlOutput{NVMeSmartHealthInformationLog: &smartmodel.SmartctlNVMeHealthLog{}}, "nvme"},
		{smartmodel.SmartctlOutput{AtaSmartAttributes: &smartmodel.AtaSmartAttributes{}}, "ata"},
		{smartmodel.SmartctlOutput{ScsiErrorCounterLog: &smartmodel.ScsiErrorCounterLog{}}, "scsi"},
		{smartmodel.SmartctlOutput{ScsiModelName: "SEAGATE"}, "scsi"},
		{smartmodel.SmartctlOutput{}, "ata"},
	} {
		assert.Equal(t, test.want, smartProtocol(&test.data), "%+v", test.data)
	}
}

func TestIsVirtualDevice(t *testing.T) {
	for _, test := range []struct {
		name                         string
		vendor, product, model, scsi string
		want                         bool
	}{
		{"regular drive", "SEAGATE", "ST1000DM003", "ST1000DM003-1CH162", "", false},
		{"regular nvme", "", "", "Samsung SSD 970 EVO Plus 1TB", "", false},
		{"qemu", "QEMU", "QEMU HARDDISK", "QEMU HARDDISK", "", true},
		{"qemu nvme", "", "", "QEMU NVMe Ctrl", "", true},
		{"virtualbox", "VBOX", "HARDDISK", "VBOX HARDDISK", "", true},
		{"vmware", "VMWARE", "Virtual disk", "VMWARE Virtual disk", "", true},
		{"virtual in model", "ATA", "VIRTUAL", "VIRTUAL DISK", "", true},
		{"iet", "IET", "VIRTUAL-DISK", "VIRTUAL-DISK", "", true},
		{"hyper-v", "MSFT", "VIRTUAL HD", "VIRTUAL HD", "", true},
		{"scsi model", "", "", "", "QEMU HARDDISK", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &smartmodel.SmartctlOutput{ScsiVendor: test.vendor, ScsiProduct: test.product, ModelName: test.model, ScsiModelName: test.scsi}
			assert.Equal(t, test.want, isVirtualDevice(data))
		})
	}
}

func TestParseSmartctlDeviceStatisticsTemperature(t *testing.T) {
	for _, test := range []struct {
		name    string
		current string
		want    uint8
	}{
		{"fallback when temperature missing", "", 22},
		// Negative entries (minimum operating temperature) must not fail the parse.
		{"reported temperature wins", `"temperature": {"current": 38},`, 38},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := fmt.Sprintf(`{
				"device": {"name": "/dev/sdb", "type": "sat"},
				"serial_number": "DEVSTAT123",
				"smart_status": {"passed": true},
				%s
				"ata_smart_attributes": {"table": []},
				"ata_device_statistics": {"pages": [{"number": 5, "table": [
					{"name": "Current Temperature", "value": 22},
					{"name": "Specified Minimum Operating Temperature", "value": -20}
				]}]}
			}`, test.current)
			assert.Equal(t, test.want, parseSmart(t, []byte(output), "").Temperature)
		})
	}
}

func TestParseSmartctlParentheticalRawValue(t *testing.T) {
	deviceData := parseSmart(t, []byte(`{
		"device": {"name": "/dev/sdz", "type": "sat"},
		"serial_number": "PARENTHESES123",
		"smart_status": {"passed": true},
		"ata_smart_attributes": {"table": [{
			"id": 9, "name": "Power_On_Hours", "value": 93, "worst": 55, "thresh": 0,
			"raw": {"value": 57891864217128, "string": "39925 (212 206 0)"}
		}]}
	}`), "")
	require.Len(t, deviceData.Attributes, 1)
	assert.Equal(t, uint64(39925), deviceData.Attributes[0].RawValue)
	assert.Equal(t, "39925 (212 206 0)", deviceData.Attributes[0].RawString)
}

// Counters larger than the old narrow Go types used to fail the decode and
// drop the whole device.
func TestParseSmartctlNvmeLargeCounters(t *testing.T) {
	deviceData := parseSmart(t, []byte(`{
		"device": {"name": "/dev/nvme0", "type": "nvme"},
		"serial_number": "NVME-LARGE",
		"smart_status": {"passed": true},
		"nvme_smart_health_information_log": {
			"temperature": 40,
			"percentage_used": 300,
			"power_cycles": 70000,
			"power_on_hours": 5000000000,
			"unsafe_shutdowns": 159754
		}
	}`), "")
	assertAttrValue(t, deviceData.Attributes, "UnsafeShutdowns", 159754)
	assertAttrValue(t, deviceData.Attributes, "PowerCycles", 70000)
	assertAttrValue(t, deviceData.Attributes, "PowerOnHours", 5000000000)
	assertAttrValue(t, deviceData.Attributes, "PercentageUsed", 300)

	health := deviceData.NVMeSmartHealthInformationLog
	require.NotNil(t, health)
	assert.Equal(t, uint64(70000), health.PowerCycles)
	assert.Equal(t, uint64(5000000000), health.PowerOnHours)
	assert.Equal(t, uint64(159754), health.UnsafeShutdowns)
	assert.Equal(t, uint64(300), health.PercentageUsed)
}

// NVMe health counters are 128-bit, so smartctl can print values past uint64.
// They read as invalid instead of failing the decode and dropping the device,
// and the health log the page shows as numbers is left out.
func TestParseSmartctlNvmeCounterBeyondUint64(t *testing.T) {
	deviceData := parseSmart(t, []byte(`{
		"device": {"name": "/dev/nvme0", "type": "nvme"},
		"serial_number": "NVME-OVERFLOW",
		"smart_status": {"passed": true},
		"nvme_smart_health_information_log": {
			"temperature": 35,
			"data_units_written": 340282366920938463463374607431768211455,
			"power_on_hours": 1234,
			"media_errors": 557325022338560884631142400
		}
	}`), "")
	for _, name := range []string{"MediaErrors", "DataUnitsWritten"} {
		attr := findAttr(deviceData.Attributes, name)
		require.NotNil(t, attr, name)
		assert.Zero(t, attr.RawValue, name)
		assert.Equal(t, smartmodel.NVMeCounterInvalid, attr.RawString, name)
	}
	assertAttrValue(t, deviceData.Attributes, "PowerOnHours", 1234)
	assert.Empty(t, findAttr(deviceData.Attributes, "PowerOnHours").RawString)
	assert.Equal(t, uint8(35), deviceData.Temperature)
	assert.Nil(t, deviceData.NVMeSmartHealthInformationLog)
}

// Every 128-bit counter reads as invalid when it overflows.
func TestParseSmartctlNvmeEveryCounterOverflow(t *testing.T) {
	fields := map[string]string{
		"data_units_read":      "DataUnitsRead",
		"data_units_written":   "DataUnitsWritten",
		"host_reads":           "HostReads",
		"host_writes":          "HostWrites",
		"controller_busy_time": "ControllerBusyTime",
		"power_cycles":         "PowerCycles",
		"power_on_hours":       "PowerOnHours",
		"unsafe_shutdowns":     "UnsafeShutdowns",
		"media_errors":         "MediaErrors",
		"num_err_log_entries":  "NumErrLogEntries",
	}
	for field, name := range fields {
		deviceData := parseSmart(t, []byte(`{
			"device": {"name": "/dev/nvme0", "type": "nvme"},
			"serial_number": "NVME-`+name+`",
			"nvme_smart_health_information_log": {"temperature": 35, "`+field+`": 18446744073709551616}
		}`), "")
		attr := findAttr(deviceData.Attributes, name)
		require.NotNil(t, attr, name)
		assert.Zero(t, attr.RawValue, name)
		assert.Equal(t, smartmodel.NVMeCounterInvalid, attr.RawString, name)
		assert.Nil(t, deviceData.NVMeSmartHealthInformationLog, name)
	}
}

// An overflowed total capacity still parses the device; the capacity is left
// unknown (0) rather than made up.
func TestParseSmartctlNvmeCapacity(t *testing.T) {
	deviceData := parseSmart(t, []byte(`{
		"device": {"name": "/dev/nvme0", "type": "nvme"},
		"serial_number": "NVME-OVERFLOW-CAP",
		"smart_status": {"passed": true},
		"nvme_total_capacity": 18446744073709551616,
		"nvme_smart_health_information_log": {
			"temperature": 35,
			"power_cycles": 120,
			"media_errors": null
		}
	}`), "")
	assert.Zero(t, deviceData.Capacity)
	assertAttrValue(t, deviceData.Attributes, "PowerCycles", 120)
	assertAttrValue(t, deviceData.Attributes, "MediaErrors", 0)

	deviceData = parseSmart(t, []byte(`{
		"device": {"name": "/dev/nvme0", "type": "nvme"},
		"serial_number": "NVME-CAP",
		"nvme_total_capacity": 1000204886016,
		"nvme_smart_health_information_log": {"temperature": 35}
	}`), "")
	assert.Equal(t, uint64(1000204886016), deviceData.Capacity)
}

// Out-of-range temperatures are clamped instead of failing the whole parse.
func TestParseSmartctlTemperatureOutOfRange(t *testing.T) {
	sata := parseSmart(t, []byte(`{
		"device": {"name": "/dev/sda", "type": "sat"},
		"serial_number": "SATA-NEG",
		"smart_status": {"passed": true},
		"temperature": {"current": -5},
		"ata_smart_attributes": {"table": []}
	}`), "")
	assert.Equal(t, uint8(0), sata.Temperature)

	nvme := parseSmart(t, []byte(`{
		"device": {"name": "/dev/nvme0", "type": "nvme"},
		"serial_number": "NVME-HOT",
		"smart_status": {"passed": true},
		"nvme_smart_health_information_log": {"temperature": 300}
	}`), "")
	assert.Equal(t, uint8(255), nvme.Temperature)
	assertAttrValue(t, nvme.Attributes, "Temperature", 255)
	require.NotNil(t, nvme.NVMeSmartHealthInformationLog)
	assert.Equal(t, int64(300), nvme.NVMeSmartHealthInformationLog.Temperature)
}

func TestParseSmartctlScsiWithoutErrorCounterLog(t *testing.T) {
	deviceData := parseSmart(t, []byte(`{
		"device": {"name": "/dev/sdb", "type": "scsi"},
		"scsi_model_name": "SEAGATE ST4000NM0023",
		"serial_number": "SCSI-NOLOG",
		"smart_status": {"passed": true},
		"temperature": {"current": 30},
		"power_on_time": {"hours": 1234}
	}`), "")
	assertAttrValue(t, deviceData.Attributes, "PowerOnHours", 1234)
	assertAttrValue(t, deviceData.Attributes, "ReadTotalUncorrectedErrors", 0)
	assert.Nil(t, findAttr(deviceData.Attributes, "ReadGigabytesProcessed"))
}

func TestParseConfiguredDevices(t *testing.T) {
	devices, err := parseConfiguredDevices("/dev/sda:SAT, /dev/nvme0:nvme,/dev/sdb")
	require.NoError(t, err)
	assert.Equal(t, []smartDevice{
		{name: "/dev/sda", dtype: "sat", explicit: true},
		{name: "/dev/nvme0", dtype: "nvme", explicit: true},
		{name: "/dev/sdb"},
	}, devices)

	t.Setenv("SMART_DEVICES_SEPARATOR", "|")
	devices, err = parseConfiguredDevices("/dev/sda:jmb39x-q,0|/dev/bus/0:megaraid,1")
	require.NoError(t, err)
	assert.Equal(t, []smartDevice{
		{name: "/dev/sda", dtype: "jmb39x-q,0", explicit: true},
		{name: "/dev/bus/0", dtype: "megaraid,1", explicit: true},
	}, devices)

	_, err = parseConfiguredDevices(":sat")
	require.Error(t, err)
	_, err = parseConfiguredDevices(" | ")
	assert.ErrorIs(t, err, errNoValidSmartData)
}

func TestMergeDevices(t *testing.T) {
	for _, test := range []struct {
		name       string
		scanned    []smartDevice
		configured []smartDevice
		want       []smartDevice
	}{
		{
			name:       "configured type replaces scanned type",
			scanned:    []smartDevice{{name: "/dev/sda", dtype: "scsi"}, {name: "/dev/nvme0", dtype: "nvme"}},
			configured: []smartDevice{{name: "/dev/sda", dtype: "sat", explicit: true}},
			want:       []smartDevice{{name: "/dev/sda", dtype: "sat", explicit: true}, {name: "/dev/nvme0", dtype: "nvme"}},
		},
		{
			name:       "configured path without type keeps scanned type",
			scanned:    []smartDevice{{name: "/dev/sda", dtype: "scsi"}},
			configured: []smartDevice{{name: "/dev/sda"}},
			want:       []smartDevice{{name: "/dev/sda", dtype: "scsi"}},
		},
		{
			name:       "same path and type marks scanned device explicit",
			scanned:    []smartDevice{{name: "/dev/sda", dtype: "scsi"}},
			configured: []smartDevice{{name: "/dev/sda", dtype: "scsi", explicit: true}},
			want:       []smartDevice{{name: "/dev/sda", dtype: "scsi", explicit: true}},
		},
		{
			name:       "unscanned devices are added",
			scanned:    []smartDevice{{name: "/dev/sda", dtype: "sat"}},
			configured: []smartDevice{{name: "/dev/sdb", dtype: "sat", explicit: true}},
			want:       []smartDevice{{name: "/dev/sda", dtype: "sat"}, {name: "/dev/sdb", dtype: "sat", explicit: true}},
		},
		{
			name: "RAID members sharing a path are added",
			scanned: []smartDevice{
				{name: "/dev/bus/0", dtype: "megaraid,0"},
				{name: "/dev/bus/0", dtype: "megaraid,1"},
			},
			configured: []smartDevice{{name: "/dev/bus/0", dtype: "megaraid,2", explicit: true}},
			want: []smartDevice{
				{name: "/dev/bus/0", dtype: "megaraid,0"},
				{name: "/dev/bus/0", dtype: "megaraid,1"},
				{name: "/dev/bus/0", dtype: "megaraid,2", explicit: true},
			},
		},
		{
			name:    "several configured types for a path scanned once",
			scanned: []smartDevice{{name: "/dev/sda", dtype: "scsi"}},
			configured: []smartDevice{
				{name: "/dev/sda", dtype: "cciss,0", explicit: true},
				{name: "/dev/sda", dtype: "cciss,1", explicit: true},
			},
			want: []smartDevice{
				{name: "/dev/sda", dtype: "cciss,0", explicit: true},
				{name: "/dev/sda", dtype: "cciss,1", explicit: true},
			},
		},
		{
			name:       "duplicates are dropped",
			scanned:    []smartDevice{{name: "/dev/sda", dtype: "sat"}, {name: "/dev/sda", dtype: "sat"}},
			configured: []smartDevice{{name: "/dev/sdb"}, {name: "/dev/sdb"}},
			want:       []smartDevice{{name: "/dev/sda", dtype: "sat"}, {name: "/dev/sdb"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, mergeDevices(test.scanned, test.configured))
		})
	}
}

func TestAttemptsFor(t *testing.T) {
	sm := &Manager{excludedDevices: map[string]struct{}{"/dev/nvme1n1": {}}}
	for _, test := range []struct {
		device smartDevice
		want   []smartAttempt
	}{
		{smartDevice{name: "/dev/sda", dtype: "sat"}, []smartAttempt{{"/dev/sda", "sat"}}},
		{smartDevice{name: "/dev/sda"}, []smartAttempt{{"/dev/sda", ""}}},
		{smartDevice{name: "/dev/sda", dtype: "ata"}, []smartAttempt{{"/dev/sda", ""}}},
		{smartDevice{name: "/dev/sda", dtype: "scsi"}, []smartAttempt{{"/dev/sda", ""}, {"/dev/sda", "sat"}}},
		{smartDevice{name: "/dev/sda", dtype: "scsi", explicit: true}, []smartAttempt{{"/dev/sda", "scsi"}}},
		{smartDevice{name: "/dev/sda", dtype: "ata", explicit: true}, []smartAttempt{{"/dev/sda", "ata"}}},
		{smartDevice{name: "/dev/bus/0", dtype: "megaraid,1"}, []smartAttempt{{"/dev/bus/0", "megaraid,1"}}},
		{smartDevice{name: "/dev/nvme0", dtype: "nvme"}, []smartAttempt{{"/dev/nvme0", "nvme"}, {"/dev/nvme0n1", "nvme"}}},
		{smartDevice{name: "/dev/nvme0", dtype: "nvme", explicit: true}, []smartAttempt{{"/dev/nvme0", "nvme"}, {"/dev/nvme0n1", "nvme"}}},
		{smartDevice{name: "/dev/nvme0"}, []smartAttempt{{"/dev/nvme0", ""}, {"/dev/nvme0n1", ""}}},
		{smartDevice{name: "/dev/nvme0", dtype: "sntasmedia"}, []smartAttempt{{"/dev/nvme0", "sntasmedia"}}},
		{smartDevice{name: "/dev/nvme0n1", dtype: "nvme"}, []smartAttempt{{"/dev/nvme0n1", "nvme"}}},
		{smartDevice{name: "/dev/nvme1", dtype: "nvme"}, []smartAttempt{{"/dev/nvme1", "nvme"}}},
	} {
		assert.Equal(t, test.want, sm.attemptsFor(test.device), "%+v", test.device)
	}
}

func TestSmartctlArgs(t *testing.T) {
	for _, test := range []struct {
		attempt smartAttempt
		ata     bool
		standby bool
		want    string
	}{
		{smartAttempt{"/dev/sda", ""}, false, true, "-a --json=c -n standby /dev/sda"},
		{smartAttempt{"/dev/sda", ""}, true, true, "-a --json=c -l devstat -n standby /dev/sda"},
		{smartAttempt{"/dev/sda", "sat"}, false, true, "-d sat -a --json=c -l devstat -n standby /dev/sda"},
		{smartAttempt{"/dev/sda", "ata"}, false, true, "-d ata -a --json=c -l devstat -n standby /dev/sda"},
		{smartAttempt{"/dev/nvme0", "nvme"}, false, true, "-d nvme -a --json=c -n standby /dev/nvme0"},
		{smartAttempt{"/dev/bus/0", "megaraid,1"}, true, true, "-d megaraid,1 -a --json=c -l devstat -n standby /dev/bus/0"},
		{smartAttempt{"/dev/sda", "sat"}, false, false, "-d sat -a --json=c -l devstat /dev/sda"},
		{smartAttempt{"/dev/nvme0", "nvme"}, false, false, "-d nvme -a --json=c /dev/nvme0"},
	} {
		assert.Equal(t, test.want, strings.Join(smartctlArgs(test.attempt, test.ata, test.standby), " "))
	}
}

// Standby is smartctl's "Device is in X mode" message with exit bit 1, not
// the exit bit alone, which also means the device could not be opened.
func TestIsStandby(t *testing.T) {
	assert.True(t, isStandby([]byte(standbyOutput), 2))
	assert.True(t, isStandby([]byte(`{"smartctl":{"messages":[{"string":"Device is in SLEEP mode, exit(2)"}]}}`), 2))
	assert.False(t, isStandby([]byte(standbyOutput), 0), "needs exit bit 1")
	assert.False(t, isStandby([]byte(`{"smartctl":{"messages":[{"string":"Permission denied"}]}}`), 2))
	assert.False(t, isStandby([]byte(openFailedOutput), 2))
	// smartctl reports SLEEP when it lacks permission to check the power mode;
	// a really sleeping disk looks the same.
	assert.True(t, isStandby(readSmartFixture(t, "sat_sleep_permission.json"), 2))
}

const (
	standbyOutput    = `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]},"device":{"name":"/dev/sda","type":"sat"}}`
	openFailedOutput = `{"smartctl":{"exit_status":2,"messages":[{"string":"Smartctl open device: /dev/sda [SAT] failed: No such device","severity":"error"}]}}`
)

// fakeSmartctl answers smartctl calls from a table keyed by "<-d type> <path>"
// (an empty type for detection, e.g. " /dev/sda") and records each call.
// Calls without -n standby are answered from wake first, if it has the key.
// A key in errs fails the run itself, as a timeout does.
type fakeSmartctl struct {
	scan      string
	scanExit  int
	scanErr   error
	responses map[string]fakeResponse
	wake      map[string]fakeResponse
	errs      map[string]error
	calls     []string
	onCall    func(call string) // runs before a device call is answered
}

type fakeResponse struct {
	output string
	exit   int
}

func (f *fakeSmartctl) run(_ context.Context, _ time.Duration, args ...string) ([]byte, int, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if call == "--scan -j" {
		return []byte(f.scan), f.scanExit, f.scanErr
	}
	if f.onCall != nil {
		f.onCall(call)
	}
	dtype := ""
	if i := slices.Index(args, "-d"); i >= 0 {
		dtype = args[i+1]
	}
	key := dtype + " " + args[len(args)-1]
	if err := f.errs[key]; err != nil {
		return nil, 0, err
	}
	response, ok := f.wake[key]
	if !ok || slices.Contains(args, "-n") {
		response, ok = f.responses[key]
	}
	if !ok {
		return []byte(openFailedOutput), 2, nil
	}
	return []byte(response.output), response.exit, nil
}

// deviceCalls returns the recorded calls other than the scan.
func (f *fakeSmartctl) deviceCalls() []string {
	return slices.DeleteFunc(slices.Clone(f.calls), func(call string) bool { return call == "--scan -j" })
}

func newFakeManager(t *testing.T, fake *fakeSmartctl) *Manager {
	t.Helper()
	original, originalSeq := scanSysfsDevices, diskSeq
	scanSysfsDevices = func() []smartDevice { return nil }
	diskSeq = func(string) uint64 { return 0 }
	t.Cleanup(func() { scanSysfsDevices, diskSeq = original, originalSeq })
	return &Manager{data: map[string]*smartmodel.SmartData{}, run: fake.run}
}

// nvmeAt returns the nvme0.json fixture as if smartctl had queried path.
func nvmeAt(t *testing.T, path string) fakeResponse {
	return fakeResponse{output: strings.ReplaceAll(string(readSmartFixture(t, "nvme0.json")), `"/dev/nvme0"`, `"`+path+`"`)}
}

func scanOf(devices ...string) string {
	entries := make([]string, 0, len(devices))
	for _, device := range devices {
		name, dtype, _ := strings.Cut(device, ":")
		entries = append(entries, fmt.Sprintf(`{"name":%q,"type":%q}`, name, dtype))
	}
	return `{"devices":[` + strings.Join(entries, ",") + `]}`
}

func TestParseScanFixture(t *testing.T) {
	devices, err := parseScan(readSmartFixture(t, "scan.json"))
	require.NoError(t, err)
	assert.Equal(t, []smartDevice{{name: "/dev/sda", dtype: "sat"}, {name: "/dev/nvme0", dtype: "nvme"}}, devices)
}

// A SATA disk the scan reports as scsi returns identity-only output when
// detected; the sat attempt recovers its data and is tried first next time.
func TestRefreshScsiFallsBackToSat(t *testing.T) {
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:scsi"),
		responses: map[string]fakeResponse{
			" /dev/sda":    {output: string(readSmartFixture(t, "scsi_identity.json"))},
			"sat /dev/sda": {output: string(readSmartFixture(t, "sat.json"))},
		},
	}
	sm := newFakeManager(t, fake)

	require.NoError(t, sm.Refresh(t.Context(), false))
	data := sm.GetCurrentData()["Z4K2A01FS"]
	assert.Equal(t, "TOSHIBA MG03ACA200", data.ModelName)
	assert.Equal(t, "sat", data.DiskType)
	assert.Equal(t, uint8(38), data.Temperature)
	assert.Equal(t, "PASSED", data.SmartStatus)
	assert.Equal(t, []string{
		"-a --json=c -n standby /dev/sda",
		"-d sat -a --json=c -l devstat -n standby /dev/sda",
	}, fake.deviceCalls())

	fake.calls = nil
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, []string{"-d sat -a --json=c -l devstat -n standby /dev/sda"}, fake.deviceCalls())
}

// A disk scanned as ata is detected but requests devstat from the first
// query, a wake query included; after that the protocol of the reading decides.
func TestRefreshScannedAtaRequestsDevstat(t *testing.T) {
	for _, test := range []struct {
		fixture, next string
		asleep        bool
	}{
		{"sda.json", "-a --json=c -l devstat -n standby /dev/sda", false},
		{"scsi.json", "-a --json=c -n standby /dev/sda", false},
		{"sda.json", "-a --json=c -l devstat -n standby /dev/sda", true},
		{"scsi.json", "-a --json=c -n standby /dev/sda", true},
	} {
		t.Run(fmt.Sprintf("%s/asleep=%t", test.fixture, test.asleep), func(t *testing.T) {
			reading := fakeResponse{output: string(readSmartFixture(t, test.fixture))}
			fake := &fakeSmartctl{
				scan:      scanOf("/dev/sda:ata"),
				responses: map[string]fakeResponse{" /dev/sda": reading},
			}
			wantCalls := []string{"-a --json=c -l devstat -n standby /dev/sda"}
			if test.asleep {
				fake.responses[" /dev/sda"] = fakeResponse{output: standbyOutput, exit: 2}
				fake.wake = map[string]fakeResponse{" /dev/sda": reading}
				wantCalls = append(wantCalls, "-a --json=c -l devstat /dev/sda")
			}
			sm := newFakeManager(t, fake)

			require.NoError(t, sm.Refresh(t.Context(), false))
			assert.Equal(t, wantCalls, fake.deviceCalls())

			fake.calls = nil
			fake.responses[" /dev/sda"] = reading
			require.NoError(t, sm.Refresh(t.Context(), false))
			assert.Equal(t, []string{test.next}, fake.deviceCalls())
		})
	}
}

// An explicit SMART_DEVICES type is a deliberate override and is not retried.
func TestRefreshExplicitTypeIsNotRetried(t *testing.T) {
	t.Setenv("SMART_DEVICES", "/dev/sda:scsi")
	fake := &fakeSmartctl{
		scan: scanOf(),
		responses: map[string]fakeResponse{
			"scsi /dev/sda": {output: string(readSmartFixture(t, "scsi_identity.json"))},
			"sat /dev/sda":  {output: string(readSmartFixture(t, "sat.json"))},
		},
	}
	sm := newFakeManager(t, fake)

	err := sm.Refresh(t.Context(), false)
	require.ErrorIs(t, err, errNoValidSmartData)
	assert.Empty(t, sm.GetCurrentData())
	assert.Contains(t, sm.failing["/dev/sda|scsi"], errNoHealthData.Error())
	assert.Equal(t, []string{"-d scsi -a --json=c -n standby /dev/sda"}, fake.deviceCalls())
}

// An NVMe controller that rejects SMART queries falls back to its namespace,
// a second attempt on the same target, which is queried directly afterwards.
func TestRefreshNvmeNamespaceFallback(t *testing.T) {
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/nvme0:nvme"),
		responses: map[string]fakeResponse{"nvme /dev/nvme0n1": nvmeAt(t, "/dev/nvme0n1")},
	}
	sm := newFakeManager(t, fake)

	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, "/dev/nvme0n1", sm.GetCurrentData()["2024031600129"].DiskName)
	assert.Equal(t, []string{
		"-d nvme -a --json=c -n standby /dev/nvme0",
		"-d nvme -a --json=c -n standby /dev/nvme0n1",
	}, fake.deviceCalls())

	// The next refresh, after the scan cache expired, goes straight to the namespace.
	fake.calls = nil
	sm.lastScan = time.Now().Add(-smartScanInterval - time.Minute)
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, []string{"-d nvme -a --json=c -n standby /dev/nvme0n1"}, fake.deviceCalls())

	// A scan that reports both names does not duplicate the drive.
	fake.scan = scanOf("/dev/nvme0:nvme", "/dev/nvme0n1:nvme")
	require.NoError(t, sm.Refresh(t.Context(), true))
	assert.Len(t, sm.GetCurrentData(), 1)
}

func TestRefreshNvmeNamespaceFailure(t *testing.T) {
	denied := fakeResponse{output: string(readSmartFixture(t, "nvme_permission_denied.json")), exit: 4}
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/nvme0:nvme"),
		responses: map[string]fakeResponse{"nvme /dev/nvme0": denied, "nvme /dev/nvme0n1": denied},
	}
	sm := newFakeManager(t, fake)

	require.ErrorIs(t, sm.Refresh(t.Context(), false), errNoValidSmartData)
	assert.Empty(t, sm.GetCurrentData())
	assert.Len(t, fake.deviceCalls(), 2)
}

func TestRefreshHonorsExclusions(t *testing.T) {
	for _, excluded := range []string{"/dev/nvme0", "/dev/nvme0n1"} {
		t.Run(excluded, func(t *testing.T) {
			fake := &fakeSmartctl{
				scan:      scanOf("/dev/nvme0:nvme"),
				responses: map[string]fakeResponse{"nvme /dev/nvme0n1": nvmeAt(t, "/dev/nvme0n1")},
			}
			sm := newFakeManager(t, fake)
			sm.excludedDevices = map[string]struct{}{excluded: {}}

			require.Error(t, sm.Refresh(t.Context(), false))
			assert.Empty(t, sm.GetCurrentData())
			assert.NotContains(t, fake.deviceCalls(), "-d nvme -a --json=c -n standby /dev/nvme0n1")
		})
	}
}

// A sleeping disk that was read before is never woken: its last record stays.
func TestRefreshStandby(t *testing.T) {
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/sda:sat"),
		responses: map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sda.json")), exit: 64}},
	}
	sm := newFakeManager(t, fake)
	require.NoError(t, sm.Refresh(t.Context(), false))

	fake.responses["sat /dev/sda"] = fakeResponse{output: standbyOutput, exit: 2}
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, "P3-2TB", sm.GetCurrentData()["9C40918040082"].ModelName)

	for _, call := range fake.deviceCalls() {
		assert.Contains(t, call, "-n standby")
	}
}

// Exit status 2 without smartctl's standby message is a device that could not
// be opened: it is not read as asleep, so its last "PASSED" does not stay.
func TestRefreshDeviceThatCannotBeOpenedIsDropped(t *testing.T) {
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:sat", "/dev/nvme0:nvme"),
		responses: map[string]fakeResponse{
			"sat /dev/sda":    {output: string(readSmartFixture(t, "sda.json"))},
			"nvme /dev/nvme0": nvmeAt(t, "/dev/nvme0"),
		},
	}
	sm := newFakeManager(t, fake)
	require.NoError(t, sm.Refresh(t.Context(), false))
	require.Contains(t, sm.GetCurrentData(), "9C40918040082")

	fake.responses["sat /dev/sda"] = fakeResponse{output: openFailedOutput, exit: 2}
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.NotContains(t, sm.GetCurrentData(), "9C40918040082")
	assert.Contains(t, sm.GetCurrentData(), "2024031600129")
	assert.Contains(t, sm.failing, "/dev/sda|sat")

	// It comes back once it answers again.
	fake.responses["sat /dev/sda"] = fakeResponse{output: string(readSmartFixture(t, "sda.json"))}
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Contains(t, sm.GetCurrentData(), "9C40918040082")
	assert.Empty(t, sm.failing)
}

// A passing read failure does not forget the last reading that a disk later
// in standby reuses, even when it was already woken once.
func TestRefreshStandbyAfterCollectionFailure(t *testing.T) {
	for _, initiallyAsleep := range []bool{false, true} {
		t.Run(fmt.Sprintf("initiallyAsleep=%t", initiallyAsleep), func(t *testing.T) {
			reading := fakeResponse{output: string(readSmartFixture(t, "sda.json"))}
			standby := fakeResponse{output: standbyOutput, exit: 2}
			fake := &fakeSmartctl{
				scan:      scanOf("/dev/sda:sat"),
				responses: map[string]fakeResponse{"sat /dev/sda": reading},
				wake:      map[string]fakeResponse{"sat /dev/sda": reading},
			}
			if initiallyAsleep {
				fake.responses["sat /dev/sda"] = standby
			}
			sm := newFakeManager(t, fake)
			require.NoError(t, sm.Refresh(t.Context(), false))
			previous := sm.GetCurrentData()

			fake.responses["sat /dev/sda"] = fakeResponse{output: `{"smartctl":{"exit_status":4}}`, exit: 4}
			for range 2 {
				require.Error(t, sm.Refresh(t.Context(), false))
				assert.Empty(t, sm.GetCurrentData(), "failed reads must not be reported as fresh data")
			}

			fake.responses["sat /dev/sda"] = standby
			fake.calls = nil
			require.NoError(t, sm.Refresh(t.Context(), false))
			assert.Equal(t, previous, sm.GetCurrentData())
			assert.Equal(t, []string{"-d sat -a --json=c -l devstat -n standby /dev/sda"}, fake.deviceCalls())
		})
	}
}

// A sleeping disk with no earlier reading is woken once so it shows up.
func TestRefreshWakesUnreadStandbyDisk(t *testing.T) {
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/sda:sat"),
		responses: map[string]fakeResponse{"sat /dev/sda": {output: standbyOutput, exit: 2}},
		wake:      map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sda.json")), exit: 64}},
	}
	sm := newFakeManager(t, fake)

	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, "P3-2TB", sm.GetCurrentData()["9C40918040082"].ModelName)
	assert.Equal(t, []string{
		"-d sat -a --json=c -l devstat -n standby /dev/sda",
		"-d sat -a --json=c -l devstat /dev/sda",
	}, fake.deviceCalls())

	// Read now, so a later standby reuses its record instead of waking it.
	fake.calls = nil
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, []string{"-d sat -a --json=c -l devstat -n standby /dev/sda"}, fake.deviceCalls())
}

// A disk whose wake query fails is not woken again on later refreshes, and is
// skipped while it sleeps.
func TestRefreshWakeFailureNotRetried(t *testing.T) {
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/sda:sat"),
		responses: map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sat_sleep_permission.json")), exit: 2}},
		wake:      map[string]fakeResponse{"sat /dev/sda": {output: `{"smartctl":{"exit_status":2,"messages":[{"string":"Permission denied"}]}}`, exit: 2}},
	}
	sm := newFakeManager(t, fake)

	require.ErrorIs(t, sm.Refresh(t.Context(), false), errNoValidSmartData)
	assert.Contains(t, sm.failing["/dev/sda|sat"], "exit status 2", "the wake query's own error is kept, not standby")
	assert.Empty(t, sm.GetCurrentData())
	assert.Equal(t, []string{
		"-d sat -a --json=c -l devstat -n standby /dev/sda",
		"-d sat -a --json=c -l devstat /dev/sda",
	}, fake.deviceCalls())

	for range 2 {
		fake.calls = nil
		require.ErrorIs(t, sm.Refresh(t.Context(), false), errNoValidSmartData)
		assert.Equal(t, []string{"-d sat -a --json=c -l devstat -n standby /dev/sda"}, fake.deviceCalls())
	}
}

// A failed wake query moves on to the next attempt, which runs with -n
// standby against the now-awake disk.
func TestRefreshWakeFallsBackToSat(t *testing.T) {
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:scsi"),
		responses: map[string]fakeResponse{
			" /dev/sda":    {output: standbyOutput, exit: 2},
			"sat /dev/sda": {output: string(readSmartFixture(t, "sat.json"))},
		},
		wake: map[string]fakeResponse{
			" /dev/sda":    {output: string(readSmartFixture(t, "scsi_identity.json"))},
			"sat /dev/sda": {output: string(readSmartFixture(t, "sat.json"))},
		},
	}
	sm := newFakeManager(t, fake)

	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Equal(t, "sat", sm.GetCurrentData()["Z4K2A01FS"].DiskType)
	// A SATL can answer the SCSI identity without spinning the disk up, so the
	// sat attempt after the wake reads the disk rather than asking about standby.
	assert.Equal(t, []string{
		"-a --json=c -n standby /dev/sda",
		"-a --json=c /dev/sda",
		"-d sat -a --json=c -l devstat /dev/sda",
	}, fake.deviceCalls())
}

// A disk in standby at the path of a disk read before is that disk only while
// the kernel numbers it the same; a swapped disk is not shown with the old
// disk's record.
func TestRefreshStandbyDoesNotReuseTheRecordOfASwappedDisk(t *testing.T) {
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/sda:sat"),
		responses: map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sda.json"))}},
	}
	sm := newFakeManager(t, fake)
	seq := uint64(5)
	diskSeq = func(string) uint64 { return seq }
	require.NoError(t, sm.Refresh(t.Context(), false))
	require.Contains(t, sm.GetCurrentData(), "9C40918040082")

	fake.responses["sat /dev/sda"] = fakeResponse{output: standbyOutput, exit: 2}
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Contains(t, sm.GetCurrentData(), "9C40918040082", "the same disk asleep keeps its record")

	// Another disk at /dev/sda, asleep: woken once for its own reading.
	seq = 6
	fake.wake = map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sat.json"))}}
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.NotContains(t, sm.GetCurrentData(), "9C40918040082")
	assert.Contains(t, sm.GetCurrentData(), "Z4K2A01FS")
}

// A wake that the end of the caller's context cuts short was not tried, so a
// later refresh may still wake the disk once.
func TestRefreshWakeCutShortStaysAvailable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	fake := &fakeSmartctl{
		scan:      scanOf("/dev/sda:sat"),
		responses: map[string]fakeResponse{"sat /dev/sda": {output: standbyOutput, exit: 2}},
	}
	sm := newFakeManager(t, fake)
	run := fake.run
	sm.run = func(runCtx context.Context, timeout time.Duration, args ...string) ([]byte, int, error) {
		if !slices.Contains(args, "standby") && !slices.Contains(args, "--scan") {
			cancel()
			return nil, 0, context.Canceled
		}
		return run(runCtx, timeout, args...)
	}
	require.ErrorIs(t, sm.Refresh(ctx, false), context.Canceled)
	assert.NotContains(t, sm.woken, "/dev/sda|sat")
}

// Records are built again on each refresh, so a removed disk disappears.
func TestRefreshForgetsRemovedDevices(t *testing.T) {
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:sat", "/dev/nvme0:nvme"),
		responses: map[string]fakeResponse{
			"sat /dev/sda":    {output: string(readSmartFixture(t, "sda.json"))},
			"nvme /dev/nvme0": nvmeAt(t, "/dev/nvme0"),
		},
	}
	sm := newFakeManager(t, fake)
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Len(t, sm.GetCurrentData(), 2)

	// A failed read keeps the standby history, but a later scan that removes
	// the disk discards it.
	delete(fake.responses, "sat /dev/sda")
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Len(t, sm.GetCurrentData(), 1)
	assert.Contains(t, sm.last, "/dev/sda|sat")

	fake.scan = scanOf("/dev/nvme0:nvme")
	require.NoError(t, sm.Refresh(t.Context(), true))
	assert.Equal(t, []string{"2024031600129"}, slices.Collect(maps.Keys(sm.GetCurrentData())))
	assert.NotContains(t, sm.last, "/dev/sda|sat")
}

// The same disk seen through two paths is reported once, by its first path.
func TestRefreshDuplicateSerial(t *testing.T) {
	sda := readSmartFixture(t, "sda.json")
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:sat", "/dev/sdb:sat"),
		responses: map[string]fakeResponse{
			"sat /dev/sda": {output: string(sda)},
			"sat /dev/sdb": {output: strings.ReplaceAll(string(sda), `"/dev/sda"`, `"/dev/sdb"`)},
		},
	}
	sm := newFakeManager(t, fake)

	for range 2 {
		require.NoError(t, sm.Refresh(t.Context(), false))
		require.Len(t, sm.GetCurrentData(), 1)
		assert.Equal(t, "/dev/sda", sm.GetCurrentData()["9C40918040082"].DiskName)
	}
}

func TestRefreshScanFailureKeepsPreviousScan(t *testing.T) {
	for _, test := range []struct {
		name, output string
		exit         int
		err          error
	}{
		{name: "timeout", err: errors.New("smartctl timed out after 10s")},
		{name: "invalid JSON", output: "invalid JSON"},
		{name: "nonzero exit without devices", output: `{"smartctl":{"exit_status":1}}`, exit: 1},
		{name: "nonzero exit with partial devices", output: scanOf("/dev/sdb:sat"), exit: 1},
		{name: "empty device list", output: scanOf()},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeSmartctl{
				scan:      scanOf("/dev/sda:sat"),
				responses: map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sda.json"))}},
			}
			sm := newFakeManager(t, fake)
			require.NoError(t, sm.Refresh(t.Context(), false))

			fake.scan, fake.scanExit, fake.scanErr = test.output, test.exit, test.err
			for _, forceScan := range []bool{true, false} {
				fake.calls = nil
				require.NoError(t, sm.Refresh(t.Context(), forceScan))
				assert.Contains(t, sm.GetCurrentData(), "9C40918040082")
				require.Error(t, sm.scanErr)
				if test.exit != 0 {
					require.ErrorContains(t, sm.scanErr, "exit status 1")
				}
				assert.Equal(t, []string{"-d sat -a --json=c -l devstat -n standby /dev/sda"}, fake.deviceCalls())
				assert.Equal(t, forceScan, slices.Contains(fake.calls, "--scan -j"))
			}

			fake.scan, fake.scanExit, fake.scanErr = scanOf("/dev/sda:sat"), 0, nil
			require.NoError(t, sm.Refresh(t.Context(), true))
			assert.NoError(t, sm.scanErr)
		})
	}
}

// A scan that fails before any scan succeeded leaves nothing to keep, so it
// is not cached and the next refresh scans again; its error carries
// smartctl's own message.
func TestRefreshFirstScanFailureIsNotCached(t *testing.T) {
	fake := &fakeSmartctl{
		scan:     `{"smartctl":{"exit_status":2,"messages":[{"string":"Smartctl open device: /dev/sdz failed","severity":"error"}]}}`,
		scanExit: 2,
	}
	sm := newFakeManager(t, fake)
	require.Error(t, sm.Refresh(t.Context(), false))
	require.ErrorContains(t, sm.scanErr, "exit status 2): Smartctl open device: /dev/sdz failed")

	fake.scan, fake.scanExit = scanOf("/dev/sda:sat"), 0
	fake.responses = map[string]fakeResponse{"sat /dev/sda": {output: string(readSmartFixture(t, "sda.json"))}}
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Contains(t, sm.GetCurrentData(), "9C40918040082")
}

// A virtual disk is skipped on its first attempt and is not a failure.
func TestRefreshSkipsAVirtualDiskQuietly(t *testing.T) {
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:scsi", "/dev/sdb:sat"),
		responses: map[string]fakeResponse{
			" /dev/sda":    {output: `{"serial_number":"V1","scsi_model_name":"QEMU HARDDISK","smart_status":{"passed":true}}`},
			"sat /dev/sdb": {output: string(readSmartFixture(t, "sda.json"))},
		},
	}
	sm := newFakeManager(t, fake)
	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Empty(t, sm.failing)
	assert.NotContains(t, fake.deviceCalls(), "-d sat -a --json=c -l devstat -n standby /dev/sda", "no second attempt")
}

func TestRefreshWithSmartDevices(t *testing.T) {
	t.Setenv("SMART_DEVICES", "/dev/sda:sat,/dev/nvme0:nvme")
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:scsi"),
		responses: map[string]fakeResponse{
			"sat /dev/sda":    {output: string(readSmartFixture(t, "sda.json"))},
			"nvme /dev/nvme0": nvmeAt(t, "/dev/nvme0"),
		},
	}
	sm := newFakeManager(t, fake)

	require.NoError(t, sm.Refresh(t.Context(), false))
	assert.Len(t, sm.GetCurrentData(), 2)
	assert.Equal(t, []string{
		"-d sat -a --json=c -l devstat -n standby /dev/sda",
		"-d nvme -a --json=c -n standby /dev/nvme0",
	}, fake.deviceCalls())
}

func TestRefreshSmartDevicesEmptyDisablesSmart(t *testing.T) {
	t.Setenv("SMART_DEVICES", "   ")
	fake := &fakeSmartctl{scan: scanOf("/dev/sda:sat")}
	sm := newFakeManager(t, fake)

	require.ErrorIs(t, sm.Refresh(t.Context(), false), errNoValidSmartData)
	assert.Empty(t, fake.calls)
}

func TestRefreshSmartDevicesInvalid(t *testing.T) {
	t.Setenv("SMART_DEVICES", ":sat")
	sm := newFakeManager(t, &fakeSmartctl{scan: scanOf()})

	assert.Error(t, sm.Refresh(t.Context(), false))
}

func TestRefreshNoDevices(t *testing.T) {
	sm := newFakeManager(t, &fakeSmartctl{scan: scanOf()})

	assert.ErrorIs(t, sm.Refresh(t.Context(), false), errNoValidSmartData)
}

// One drive that fails or passes its time limit does not end the refresh: the
// drives after it, md arrays and eMMC included, are still read.
func TestRefreshMovesOnAfterDriveTimeout(t *testing.T) {
	fakeMdraidSysfs(t)
	fakeEmmcSysfs(t)
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:sat", "/dev/sdb:sat"),
		responses: map[string]fakeResponse{
			"sat /dev/sdb": {output: strings.ReplaceAll(string(readSmartFixture(t, "sat.json")), `"/dev/sda"`, `"/dev/sdb"`)},
		},
		errs: map[string]error{"sat /dev/sda": errors.New("smartctl timed out after 15s")},
	}
	sm := &Manager{data: map[string]*smartmodel.SmartData{}, run: fake.run}

	require.NoError(t, sm.Refresh(t.Context(), false))
	data := sm.GetCurrentData()
	assert.Contains(t, data, "Z4K2A01FS", "the drive after the timeout")
	assert.Contains(t, data, "mdraid:md0")
	assert.Contains(t, data, "01234567", "the eMMC device")
	assert.NotContains(t, data, "9C40918040082")
	assert.Contains(t, sm.failing["/dev/sda|sat"], "timed out")
}

// The end of the caller's context stops the refresh and keeps the records of
// the previous one.
func TestRefreshStopsWhenContextEnds(t *testing.T) {
	fake := &fakeSmartctl{
		scan: scanOf("/dev/sda:sat", "/dev/nvme0:nvme"),
		responses: map[string]fakeResponse{
			"sat /dev/sda":    {output: string(readSmartFixture(t, "sda.json"))},
			"nvme /dev/nvme0": nvmeAt(t, "/dev/nvme0"),
		},
	}
	sm := newFakeManager(t, fake)
	require.NoError(t, sm.Refresh(t.Context(), false))
	previous := sm.GetCurrentData()

	ctx, cancel := context.WithCancel(t.Context())
	fake.calls = nil
	fake.onCall = func(string) { cancel() }
	delete(fake.responses, "sat /dev/sda")

	require.ErrorIs(t, sm.Refresh(ctx, false), context.Canceled)
	assert.Equal(t, previous, sm.GetCurrentData())
	assert.Len(t, fake.deviceCalls(), 1, "nothing runs after the context ended")
}

// writeFakeSmartctl writes a shell script to stand in for smartctl.
func writeFakeSmartctl(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smartctl")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755))
	return path
}

// smartctl passing its time limit is an error of its own, told apart from the
// end of the caller's context, so the refresh moves on to the next drive.
func TestRunSmartctlTimeoutIsNotTheCallersCancel(t *testing.T) {
	// exec, so the kill reaches the process holding the output pipe.
	sm := &Manager{smartctlPath: writeFakeSmartctl(t, "exec sleep 5\n")}

	_, _, err := sm.runSmartctl(t.Context(), 50*time.Millisecond, "-a", "/dev/sda")
	require.ErrorContains(t, err, "timed out")
	require.NotErrorIs(t, err, context.DeadlineExceeded)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = sm.runSmartctl(ctx, time.Minute, "-a", "/dev/sda")
	assert.ErrorIs(t, err, context.Canceled)
}

// A non-zero exit status is smartctl's bitmask, returned with its output.
func TestRunSmartctlReturnsExitStatus(t *testing.T) {
	sm := &Manager{smartctlPath: writeFakeSmartctl(t, "echo '{\"serial_number\":\"X\"}'\nexit 64\n")}

	output, exitCode, err := sm.runSmartctl(t.Context(), time.Minute, "-a", "/dev/sda")
	require.NoError(t, err)
	assert.Equal(t, 64, exitCode)
	assert.JSONEq(t, `{"serial_number":"X"}`, string(output))
}

func TestFindAtaDeviceStatisticsValue(t *testing.T) {
	val42 := int64(42)
	val100 := int64(100)
	valMinus20 := int64(-20)

	tests := []struct {
		name           string
		data           smartmodel.SmartctlOutput
		ataDeviceStats smartmodel.AtaDeviceStatistics
		entryNumber    uint8
		entryName      string
		minValue       int64
		maxValue       int64
		expectedValue  *int64
	}{
		{
			name: "value in ataDeviceStats",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 5,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "Current Temperature", Value: &val42},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      100,
			expectedValue: &val42,
		},
		{
			name: "value unmarshaled from data",
			data: smartmodel.SmartctlOutput{
				AtaDeviceStatistics: []byte(`{"pages":[{"number":5,"table":[{"name":"Current Temperature","value":100}]}]}`),
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      255,
			expectedValue: &val100,
		},
		{
			name: "value out of range (too high)",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 5,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "Current Temperature", Value: &val100},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      50,
			expectedValue: nil,
		},
		{
			name: "value out of range (too low)",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 5,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "Min Temp", Value: &valMinus20},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Min Temp",
			minValue:      0,
			maxValue:      100,
			expectedValue: nil,
		},
		{
			name:          "no statistics available",
			data:          smartmodel.SmartctlOutput{},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      255,
			expectedValue: nil,
		},
		{
			name: "wrong page number",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 1,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "Current Temperature", Value: &val42},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      100,
			expectedValue: nil,
		},
		{
			name: "wrong entry name",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 5,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "Other Stat", Value: &val42},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      100,
			expectedValue: nil,
		},
		{
			name: "case insensitive name match",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 5,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "CURRENT TEMPERATURE", Value: &val42},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      100,
			expectedValue: &val42,
		},
		{
			name: "entry value is nil",
			ataDeviceStats: smartmodel.AtaDeviceStatistics{
				Pages: []smartmodel.AtaDeviceStatisticsPage{
					{
						Number: 5,
						Table: []smartmodel.AtaDeviceStatisticsEntry{
							{Name: "Current Temperature", Value: nil},
						},
					},
				},
			},
			entryNumber:   5,
			entryName:     "Current Temperature",
			minValue:      0,
			maxValue:      100,
			expectedValue: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findAtaDeviceStatisticsValue(&tt.data, &tt.ataDeviceStats, tt.entryNumber, tt.entryName, tt.minValue, tt.maxValue)
			if tt.expectedValue == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expectedValue, *result)
			}
		})
	}
}

func TestRefreshExcludedDevices(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		expectedDevs map[string]struct{}
	}{
		{
			name:         "empty env",
			envValue:     "",
			expectedDevs: map[string]struct{}{},
		},
		{
			name:     "single device",
			envValue: "/dev/sda",
			expectedDevs: map[string]struct{}{
				"/dev/sda": {},
			},
		},
		{
			name:     "multiple devices",
			envValue: "/dev/sda,/dev/sdb,/dev/nvme0",
			expectedDevs: map[string]struct{}{
				"/dev/sda":   {},
				"/dev/sdb":   {},
				"/dev/nvme0": {},
			},
		},
		{
			name:     "devices with whitespace",
			envValue: " /dev/sda , /dev/sdb ,  /dev/nvme0  ",
			expectedDevs: map[string]struct{}{
				"/dev/sda":   {},
				"/dev/sdb":   {},
				"/dev/nvme0": {},
			},
		},
		{
			name:     "duplicate devices",
			envValue: "/dev/sda,/dev/sdb,/dev/sda",
			expectedDevs: map[string]struct{}{
				"/dev/sda": {},
				"/dev/sdb": {},
			},
		},
		{
			name:     "empty entries and whitespace",
			envValue: "/dev/sda,, /dev/sdb , , ",
			expectedDevs: map[string]struct{}{
				"/dev/sda": {},
				"/dev/sdb": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("EXCLUDE_SMART", tt.envValue)
			} else {
				// Ensure env var is not set for empty test
				t.Setenv("EXCLUDE_SMART", "")
			}

			sm := &Manager{}
			sm.refreshExcludedDevices()

			assert.Equal(t, tt.expectedDevs, sm.excludedDevices)
		})
	}
}

func TestIsExcludedDevice(t *testing.T) {
	sm := &Manager{
		excludedDevices: map[string]struct{}{
			"/dev/sda":   {},
			"/dev/nvme0": {},
		},
	}

	tests := []struct {
		name         string
		deviceName   string
		expectedBool bool
	}{
		{"excluded device sda", "/dev/sda", true},
		{"excluded device nvme0", "/dev/nvme0", true},
		{"non-excluded device sdb", "/dev/sdb", false},
		{"non-excluded device nvme1", "/dev/nvme1", false},
		{"empty device name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sm.isExcludedDevice(tt.deviceName)
			assert.Equal(t, tt.expectedBool, result)
		})
	}
}

func TestIsNvmeControllerPath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		// Controller paths (should return true)
		{"/dev/nvme0", true},
		{"/dev/nvme1", true},
		{"/dev/nvme10", true},
		{"nvme0", true},

		// Namespace paths (should return false)
		{"/dev/nvme0n1", false},
		{"/dev/nvme1n1", false},
		{"/dev/nvme0n1p1", false},
		{"nvme0n1", false},

		// Non-NVMe paths (should return false)
		{"/dev/sda", false},
		{"/dev/sda1", false},
		{"/dev/hda", false},
		{"", false},
		{"/dev/nvme", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsNvmeControllerPath(tt.path)
			assert.Equal(t, tt.expected, result, "path: %s", tt.path)
		})
	}
}
