package smart

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SmartctlOutput holds the fields read from `smartctl -a --json` for any
// protocol. Sections that do not apply to a device are left nil or empty.
//
// A value that overflows its Go type makes json.Unmarshal fail and drops the
// whole device, so numbers use a wide type, or NVMeCounter for the 128-bit
// NVMe fields, rather than a narrow one.
type SmartctlOutput struct {
	Device          DeviceInfo   `json:"device"`
	ModelName       string       `json:"model_name"`
	SerialNumber    string       `json:"serial_number"`
	FirmwareVersion string       `json:"firmware_version"`
	UserCapacity    UserCapacity `json:"user_capacity"`
	SmartStatus     struct {
		Passed *bool `json:"passed"` // nil when smartctl could not read the health status
	} `json:"smart_status"`
	Temperature struct {
		Current int64 `json:"current"`
	} `json:"temperature"`
	PowerOnTime     *SmartPowerOnTime `json:"power_on_time"`
	PowerCycleCount *uint64           `json:"power_cycle_count"`

	// ATA
	AtaSmartAttributes  *AtaSmartAttributes `json:"ata_smart_attributes"`
	AtaSmartSelfTestLog *SmartSelfTestLog   `json:"ata_smart_self_test_log"`
	AtaDeviceStatistics jsontext.Value      `json:"ata_device_statistics"` // decoded only when needed

	// NVMe
	NVMeTotalCapacity             NVMeCounter            `json:"nvme_total_capacity"`
	NVMeVersion                   NVMeVersionInfo        `json:"nvme_version"`
	NVMeNumberOfNamespaces        uint64                 `json:"nvme_number_of_namespaces"`
	NVMeSmartHealthInformationLog *SmartctlNVMeHealthLog `json:"nvme_smart_health_information_log"`
	NVMeSelfTestLog               *NVMeSelfTestLog       `json:"nvme_self_test_log"`

	// SCSI (scsi_vendor and scsi_product are also reported for SATA disks behind SAT)
	ScsiVendor                string                    `json:"scsi_vendor"`
	ScsiProduct               string                    `json:"scsi_product"`
	ScsiModelName             string                    `json:"scsi_model_name"`
	ScsiRevision              string                    `json:"scsi_revision"`
	ScsiStartStopCycleCounter ScsiStartStopCycleCounter `json:"scsi_start_stop_cycle_counter"`
	ScsiGrownDefectList       uint64                    `json:"scsi_grown_defect_list"`
	ScsiErrorCounterLog       *ScsiErrorCounterLog      `json:"scsi_error_counter_log"`
}

type DeviceInfo struct {
	Name     string `json:"name"`
	InfoName string `json:"info_name"`
	Type     string `json:"type"`
	Protocol string `json:"protocol"`
}

type UserCapacity struct {
	Blocks uint64 `json:"blocks"`
	Bytes  uint64 `json:"bytes"`
}

type AtaSmartAttributes struct {
	Table []AtaSmartAttribute `json:"table"`
}

// SmartValue is the common object emitted by smartctl for values that may be
// represented as either a scalar or an object. It keeps the human-readable
// string and status details needed by the storage UI without retaining raw
// JSON in the daemon API.
type SmartValue struct {
	Value            *int   `json:"value,omitzero"`
	String           string `json:"string,omitempty"`
	Passed           *bool  `json:"passed,omitzero"`
	RemainingPercent *int   `json:"remaining_percent,omitzero"`
}

func (v *SmartValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*v = SmartValue{}
		return nil
	}
	var object struct {
		Value            *int   `json:"value"`
		String           string `json:"string"`
		Passed           *bool  `json:"passed"`
		RemainingPercent *int   `json:"remaining_percent"`
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &object, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)); err != nil {
			return err
		}
		v.Value = object.Value
		v.String = object.String
		v.Passed = object.Passed
		v.RemainingPercent = object.RemainingPercent
		return nil
	}
	var scalar int
	if err := json.Unmarshal(trimmed, &scalar, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)); err != nil {
		return err
	}
	v.Value = &scalar
	return nil
}

type SmartPowerOnTime struct {
	Hours   uint64 `json:"hours,omitzero"`
	Minutes uint64 `json:"minutes,omitzero"`
}

type SmartSelfTestLog struct {
	Standard *SmartSelfTestStandardLog `json:"standard,omitzero"`
}

type SmartSelfTestStandardLog struct {
	Revision           int                  `json:"revision,omitzero"`
	Table              []SmartSelfTestEntry `json:"table,omitempty"`
	Count              int                  `json:"count,omitzero"`
	ErrorCountTotal    int                  `json:"error_count_total,omitzero"`
	ErrorCountOutdated int                  `json:"error_count_outdated,omitzero"`
}

type SmartSelfTestEntry struct {
	Num           int         `json:"num,omitzero"`
	Type          *SmartValue `json:"type,omitzero"`
	Status        *SmartValue `json:"status,omitzero"`
	LifetimeHours uint64      `json:"lifetime_hours,omitzero"`
}

type NVMeSelfTestLog struct {
	CurrentSelfTestOp                *SmartValue            `json:"current_self_test_op,omitzero"`
	CurrentSelfTestCompletionPercent *SmartValue            `json:"current_self_test_completion_percent,omitzero"`
	CurrentSelfTestOperation         *SmartValue            `json:"current_self_test_operation,omitzero"`
	CurrentSelfTestCompletion        *SmartValue            `json:"current_self_test_completion,omitzero"`
	Table                            []NVMeSelfTestLogEntry `json:"table,omitempty"`
}

type NVMeSelfTestLogEntry struct {
	PowerOnHours   *SmartValue `json:"power_on_hours,omitzero"`
	SelfTestCode   *SmartValue `json:"self_test_code,omitzero"`
	SelfTestResult *SmartValue `json:"self_test_result,omitzero"`
}

type NVMeVersionInfo struct {
	String string `json:"string,omitempty"`
	Value  int    `json:"value,omitzero"`
}

type AtaDeviceStatistics struct {
	Pages []AtaDeviceStatisticsPage `json:"pages"`
}

type AtaDeviceStatisticsPage struct {
	Number uint8                      `json:"number"`
	Table  []AtaDeviceStatisticsEntry `json:"table"`
}

type AtaDeviceStatisticsEntry struct {
	Name  string `json:"name"`
	Value *int64 `json:"value,omitzero"`
}

type AtaSmartAttribute struct {
	ID         uint16   `json:"id"`
	Name       string   `json:"name"`
	Value      uint16   `json:"value"`
	Worst      uint16   `json:"worst"`
	Thresh     uint16   `json:"thresh"`
	WhenFailed string   `json:"when_failed"`
	Raw        RawValue `json:"raw"`
}

type RawValue struct {
	Value  SmartRawValue `json:"value"`
	String string        `json:"string"`
}

func (r *RawValue) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Value  jsontext.Value `json:"value"`
		String string         `json:"string"`
	}

	if err := json.Unmarshal(data, &tmp, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)); err != nil {
		return err
	}

	if len(tmp.Value) > 0 {
		if err := r.Value.UnmarshalJSON(tmp.Value); err != nil {
			return err
		}
	} else {
		r.Value = 0
	}

	r.String = tmp.String

	if parsed, ok := ParseSmartRawValueString(tmp.String); ok {
		r.Value = SmartRawValue(parsed)
	}

	return nil
}

type SmartRawValue uint64

// handles when drives report strings like "0h+0m+0.000s" or "7344 (253d 8h)" for power on hours
func (v *SmartRawValue) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || trimmed == "null" {
		*v = 0
		return nil
	}

	if trimmed[0] == '"' {
		valueStr, err := strconv.Unquote(trimmed)
		if err != nil {
			return err
		}
		parsed, ok := ParseSmartRawValueString(valueStr)
		if ok {
			*v = SmartRawValue(parsed)
			return nil
		}
		*v = 0
		return nil
	}

	if parsed, err := strconv.ParseUint(trimmed, 0, 64); err == nil {
		*v = SmartRawValue(parsed)
		return nil
	}

	if parsed, ok := ParseSmartRawValueString(trimmed); ok {
		*v = SmartRawValue(parsed)
		return nil
	}

	*v = 0
	return nil
}

// ParseSmartRawValueString attempts to extract a numeric value from the raw value
// strings emitted by smartctl, which sometimes include human-friendly annotations
// like "7344 (253d 8h)" or "0h+0m+0.000s". It returns the parsed value and a
// boolean indicating success.
func ParseSmartRawValueString(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if parsed, err := strconv.ParseUint(value, 0, 64); err == nil {
		return parsed, true
	}

	if idx := strings.IndexRune(value, 'h'); idx > 0 {
		hoursPart := strings.TrimSpace(value[:idx])
		if hoursPart != "" {
			if parsed, err := strconv.ParseFloat(hoursPart, 64); err == nil {
				return uint64(parsed), true
			}
		}
	}

	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			continue
		}
		end := i + 1
		for end < len(value) && value[end] >= '0' && value[end] <= '9' {
			end++
		}
		digits := value[i:end]
		if parsed, err := strconv.ParseUint(digits, 10, 64); err == nil {
			return parsed, true
		}
		i = end
	}

	return 0, false
}

type ScsiErrorCounter struct {
	TotalErrorsCorrected           uint64 `json:"total_errors_corrected"`
	CorrectionAlgorithmInvocations uint64 `json:"correction_algorithm_invocations"`
	GigabytesProcessed             string `json:"gigabytes_processed"`
	TotalUncorrectedErrors         uint64 `json:"total_uncorrected_errors"`
}

type ScsiErrorCounterLog struct {
	Read   ScsiErrorCounter `json:"read"`
	Write  ScsiErrorCounter `json:"write"`
	Verify ScsiErrorCounter `json:"verify"`
}

type ScsiStartStopCycleCounter struct {
	SpecifiedCycleCountOverDeviceLifetime      uint64 `json:"specified_cycle_count_over_device_lifetime"`
	AccumulatedStartStopCycles                 uint64 `json:"accumulated_start_stop_cycles"`
	SpecifiedLoadUnloadCountOverDeviceLifetime uint64 `json:"specified_load_unload_count_over_device_lifetime"`
	AccumulatedLoadUnloadCycles                uint64 `json:"accumulated_load_unload_cycles"`
}

// SmartctlNVMeHealthLog is the NVMe health log as smartctl prints it. Its
// counters are 128-bit in the NVMe specification, so they decode into
// NVMeCounter; the record keeps NVMeSmartHealthInformationLog.
type SmartctlNVMeHealthLog struct {
	NSID                    int64       `json:"nsid"`
	CriticalWarning         uint64      `json:"critical_warning"`
	Temperature             int64       `json:"temperature"`
	AvailableSpare          uint64      `json:"available_spare"`
	AvailableSpareThreshold uint64      `json:"available_spare_threshold"`
	PercentageUsed          uint64      `json:"percentage_used"`
	DataUnitsRead           NVMeCounter `json:"data_units_read"`
	DataUnitsWritten        NVMeCounter `json:"data_units_written"`
	HostReads               NVMeCounter `json:"host_reads"`
	HostWrites              NVMeCounter `json:"host_writes"`
	ControllerBusyTime      NVMeCounter `json:"controller_busy_time"`
	PowerCycles             NVMeCounter `json:"power_cycles"`
	PowerOnHours            NVMeCounter `json:"power_on_hours"`
	UnsafeShutdowns         NVMeCounter `json:"unsafe_shutdowns"`
	MediaErrors             NVMeCounter `json:"media_errors"`
	NumErrLogEntries        NVMeCounter `json:"num_err_log_entries"`
	WarningTempTime         uint64      `json:"warning_temp_time"`
	CriticalCompTime        uint64      `json:"critical_comp_time"`
	TemperatureSensors      []int64     `json:"temperature_sensors"`
}

// NVMeSmartHealthInformationLog is the NVMe health log of a SMART record,
// which the storage page shows as it is.
type NVMeSmartHealthInformationLog struct {
	NSID                    int64   `json:"nsid,omitzero"`
	CriticalWarning         uint64  `json:"critical_warning"`
	Temperature             int64   `json:"temperature"`
	AvailableSpare          uint64  `json:"available_spare"`
	AvailableSpareThreshold uint64  `json:"available_spare_threshold"`
	PercentageUsed          uint64  `json:"percentage_used"`
	DataUnitsRead           uint64  `json:"data_units_read"`
	DataUnitsWritten        uint64  `json:"data_units_written"`
	HostReads               uint64  `json:"host_reads"`
	HostWrites              uint64  `json:"host_writes"`
	ControllerBusyTime      uint64  `json:"controller_busy_time"`
	PowerCycles             uint64  `json:"power_cycles"`
	PowerOnHours            uint64  `json:"power_on_hours"`
	UnsafeShutdowns         uint64  `json:"unsafe_shutdowns"`
	MediaErrors             uint64  `json:"media_errors"`
	NumErrLogEntries        uint64  `json:"num_err_log_entries"`
	WarningTempTime         uint64  `json:"warning_temp_time"`
	CriticalCompTime        uint64  `json:"critical_comp_time"`
	TemperatureSensors      []int64 `json:"temperature_sensors"`
}

// NVMeCounter holds one of the 128-bit fields smartctl reports for NVMe
// drives (health log counters, total capacity). smartctl prints the full
// value, so faulty firmware can report a number past uint64. Such a value
// decodes as Overflow with Value 0 instead of failing json.Unmarshal and
// dropping the device, so it is never shown as a real reading.
type NVMeCounter struct {
	Value    uint64
	Overflow bool
}

// NVMeCounterInvalid is the RawString of an attribute whose counter overflowed.
const NVMeCounterInvalid = "invalid (out of range)"

func (c *NVMeCounter) UnmarshalJSON(data []byte) error {
	*c = NVMeCounter{}
	s := string(bytes.TrimSpace(data))
	if s == "null" {
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if errors.Is(err, strconv.ErrRange) {
		c.Overflow = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("invalid NVMe counter %q: %w", s, err)
	}
	c.Value = v
	return nil
}

type SmartData struct {
	ModelName       string            `json:"model_name,omitempty"`
	SerialNumber    string            `json:"serial_number,omitempty"`
	FirmwareVersion string            `json:"firmware_version,omitempty"`
	Capacity        uint64            `json:"capacity_bytes,omitzero"`
	SmartStatus     string            `json:"smart_status,omitempty"`
	DiskName        string            `json:"disk_name,omitempty"`
	DiskType        string            `json:"disk_type,omitempty"`
	Temperature     uint8             `json:"temperature_celsius,omitzero"`
	Attributes      []*SmartAttribute `json:"attributes,omitempty"`
	// The following fields are live-only compatibility data for the storage UI.
	// WriteSmartDevices projects them out of persisted history JSON so existing
	// records retain their compact shape.
	Device                        *DeviceInfo                    `json:"device,omitzero"`
	AtaSmartSelfTestLog           *SmartSelfTestLog              `json:"ata_smart_self_test_log,omitzero"`
	NVMeVersion                   *NVMeVersionInfo               `json:"nvme_version,omitzero"`
	NVMeNumberOfNamespaces        *uint64                        `json:"nvme_number_of_namespaces,omitzero"`
	NVMeSmartHealthInformationLog *NVMeSmartHealthInformationLog `json:"nvme_smart_health_information_log,omitzero"`
	NVMeSelfTestLog               *NVMeSelfTestLog               `json:"nvme_self_test_log,omitzero"`
	PowerOnTime                   *SmartPowerOnTime              `json:"power_on_time,omitzero"`
	PowerCycleCount               *uint64                        `json:"power_cycle_count,omitzero"`
}

type SmartAttribute struct {
	ID         uint16 `json:"id,omitzero"`
	Name       string `json:"name"`
	Value      uint16 `json:"value,omitzero"`
	Worst      uint16 `json:"worst,omitzero"`
	Threshold  uint16 `json:"threshold,omitzero"`
	RawValue   uint64 `json:"raw_value"`
	RawString  string `json:"raw_string,omitempty"`
	WhenFailed string `json:"when_failed,omitempty"`
}
