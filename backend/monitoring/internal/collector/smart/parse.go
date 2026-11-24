package smart

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"

	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

var errNoHealthData = errors.New("no SMART health data in smartctl output")

// unmarshalSmartctl decodes smartctl JSON leniently: smartctl prints vendor
// strings as they are, which are not always valid UTF-8.
func unmarshalSmartctl(output []byte, v any) error {
	return json.Unmarshal(output, v, json.MatchCaseInsensitiveNames(true), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
}

// parseSmartctl turns `smartctl -a --json` output into a SMART record and
// returns the protocol it was parsed as ("nvme", "scsi" or "ata"). dtype is the
// -d value used for the query; when set it is reported as the disk type.
//
// The protocol comes from smartctl's device.protocol, falling back to the
// sections present. Output that identifies a disk but holds no health data for
// its protocol is rejected, so the next smartctl attempt can run instead of a
// record of zeros being stored.
func parseSmartctl(output []byte, dtype string) (*smartmodel.SmartData, string, error) {
	data, err := decodeSmartctlOutput(output)
	if err != nil {
		return nil, "", err
	}

	protocol := smartProtocol(data)
	var smartData *smartmodel.SmartData
	switch protocol {
	case "nvme":
		smartData, err = nvmeSmartData(data, dtype)
	case "scsi":
		smartData, err = scsiSmartData(data, dtype)
	default:
		smartData, err = ataSmartData(data, dtype)
	}
	if err != nil {
		return nil, protocol, err
	}
	return smartData, protocol, nil
}

func nvmeSmartData(data *smartmodel.SmartctlOutput, dtype string) (*smartmodel.SmartData, error) {
	log := data.NVMeSmartHealthInformationLog
	if log == nil {
		return nil, errNoHealthData
	}
	smartData := newSmartData(data, dtype, data.ModelName, data.FirmwareVersion, log.Temperature)
	if smartData.Capacity == 0 {
		smartData.Capacity = data.NVMeTotalCapacity.Value
	}
	smartData.Attributes = nvmeAttributes(log, smartData.Temperature)
	version := data.NVMeVersion
	smartData.NVMeVersion = &version
	if namespaces := data.NVMeNumberOfNamespaces; namespaces != 0 {
		smartData.NVMeNumberOfNamespaces = &namespaces
	}
	smartData.NVMeSelfTestLog = data.NVMeSelfTestLog
	smartData.NVMeSmartHealthInformationLog = nvmeHealthRecord(log)
	return smartData, nil
}

func scsiSmartData(data *smartmodel.SmartctlOutput, dtype string) (*smartmodel.SmartData, error) {
	if data.SmartStatus.Passed == nil && data.ScsiErrorCounterLog == nil && data.Temperature.Current == 0 {
		return nil, errNoHealthData
	}
	smartData := newSmartData(data, dtype, data.ScsiModelName, data.ScsiRevision, data.Temperature.Current)
	smartData.Attributes = scsiAttributes(data)
	return smartData, nil
}

func ataSmartData(data *smartmodel.SmartctlOutput, dtype string) (*smartmodel.SmartData, error) {
	var table []smartmodel.AtaSmartAttribute
	if data.AtaSmartAttributes != nil {
		table = data.AtaSmartAttributes.Table
	}
	if len(table) == 0 && data.SmartStatus.Passed == nil && data.Temperature.Current == 0 && !hasJSONValue(data.AtaDeviceStatistics) {
		return nil, errNoHealthData
	}
	smartData := newSmartData(data, dtype, data.ModelName, data.FirmwareVersion, data.Temperature.Current)
	if smartData.Temperature == 0 {
		var ataDeviceStats smartmodel.AtaDeviceStatistics
		if temp := findAtaDeviceStatisticsValue(data, &ataDeviceStats, 5, "Current Temperature", 0, 255); temp != nil {
			smartData.Temperature = uint8(*temp)
		}
	}
	smartData.Attributes = ataAttributes(table)
	smartData.AtaSmartSelfTestLog = data.AtaSmartSelfTestLog
	return smartData, nil
}

// smartProtocol returns "nvme", "scsi" or "ata" for a smartctl response.
func smartProtocol(data *smartmodel.SmartctlOutput) string {
	switch strings.ToUpper(data.Device.Protocol) {
	case "NVME":
		return "nvme"
	case "SCSI":
		return "scsi"
	case "ATA":
		return "ata"
	}
	switch {
	case data.NVMeSmartHealthInformationLog != nil, strings.EqualFold(data.Device.Type, "nvme"):
		return "nvme"
	case data.AtaSmartAttributes != nil:
		return "ata"
	case data.ScsiErrorCounterLog != nil, data.ScsiModelName != "":
		return "scsi"
	default:
		return "ata"
	}
}

// hasJSONValue reports whether a raw JSON value is present and not null.
func hasJSONValue(raw jsontext.Value) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

// isVirtualDevice reports whether a device is a virtual disk that is left out
// (Kubernetes PVCs, QEMU, VirtualBox, VMware, Hyper-V, iSCSI targets).
func isVirtualDevice(data *smartmodel.SmartctlOutput) bool {
	for _, field := range []string{data.ScsiVendor, data.ScsiProduct, data.ModelName, data.ScsiModelName} {
		field = strings.ToUpper(field)
		switch {
		case strings.Contains(field, "IET"), // iSCSI Enterprise Target
			strings.Contains(field, "VIRTUAL"),
			strings.Contains(field, "QEMU"),
			strings.Contains(field, "VBOX"),
			strings.Contains(field, "VMWARE"),
			strings.Contains(field, "MSFT"): // Microsoft Hyper-V
			return true
		}
	}
	return false
}

// decodeSmartctlOutput unmarshals smartctl JSON output and rejects devices
// without a serial number and virtual devices.
func decodeSmartctlOutput(output []byte) (*smartmodel.SmartctlOutput, error) {
	var data smartmodel.SmartctlOutput
	if err := unmarshalSmartctl(output, &data); err != nil {
		return nil, fmt.Errorf("invalid smartctl JSON: %w", err)
	}
	if data.SerialNumber == "" {
		return nil, errors.New("no serial number")
	}
	if isVirtualDevice(&data) {
		return nil, fmt.Errorf("%w %q", errVirtualDevice, cmp.Or(data.ModelName, data.ScsiModelName))
	}
	return &data, nil
}

// smartctlMessages returns smartctl's own messages from its JSON output, as
// ": first; second", or "" when it printed none.
func smartctlMessages(output []byte) string {
	var report struct {
		Smartctl struct {
			Messages []struct {
				String string `json:"string"`
			} `json:"messages"`
		} `json:"smartctl"`
	}
	if unmarshalSmartctl(output, &report) != nil {
		return ""
	}
	texts := make([]string, 0, len(report.Smartctl.Messages))
	for _, message := range report.Smartctl.Messages {
		if text := strings.TrimSpace(message.String); text != "" {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	return ": " + strings.Join(texts, "; ")
}

// newSmartData fills the fields shared by all smartctl protocols. deviceType
// is the exact type used to query the device; when set, it takes precedence
// over the generic type smartctl reports.
func newSmartData(data *smartmodel.SmartctlOutput, deviceType, model, firmware string, temperature int64) *smartmodel.SmartData {
	device := data.Device
	smartData := &smartmodel.SmartData{
		ModelName:       model,
		SerialNumber:    data.SerialNumber,
		FirmwareVersion: firmware,
		Capacity:        data.UserCapacity.Bytes,
		Temperature:     uint8(min(max(temperature, 0), 255)),
		SmartStatus:     getSmartStatus(data.SmartStatus.Passed),
		DiskName:        data.Device.Name,
		DiskType:        data.Device.Type,
		Device:          &device,
		PowerOnTime:     data.PowerOnTime,
		PowerCycleCount: data.PowerCycleCount,
	}
	if deviceType != "" {
		smartData.DiskType = deviceType
	}
	return smartData
}

// getSmartStatus maps smartctl's health check result. A missing result is
// UNKNOWN rather than FAILED, so an unreadable status never reads as a failure.
func getSmartStatus(passed *bool) string {
	switch {
	case passed == nil:
		return "UNKNOWN"
	case *passed:
		return "PASSED"
	default:
		return "FAILED"
	}
}

func ataAttributes(table []smartmodel.AtaSmartAttribute) []*smartmodel.SmartAttribute {
	attributes := make([]*smartmodel.SmartAttribute, 0, len(table))
	for _, attr := range table {
		attributes = append(attributes, &smartmodel.SmartAttribute{
			ID:         attr.ID,
			Name:       attr.Name,
			Value:      attr.Value,
			Worst:      attr.Worst,
			Threshold:  attr.Thresh,
			RawValue:   uint64(attr.Raw.Value), // already parsed from raw.string when possible
			RawString:  attr.Raw.String,
			WhenFailed: attr.WhenFailed,
		})
	}
	return attributes
}

// findAtaDeviceStatisticsValue looks up one entry of the ATA device statistics
// (smartctl -l devstat), decoding them into ataDeviceStats on first use so
// further lookups can share the decoded pages.
func findAtaDeviceStatisticsValue(data *smartmodel.SmartctlOutput, ataDeviceStats *smartmodel.AtaDeviceStatistics, entryNumber uint8, entryName string, minValue, maxValue int64) *int64 {
	if len(ataDeviceStats.Pages) == 0 {
		if len(data.AtaDeviceStatistics) == 0 {
			return nil
		}
		if err := unmarshalSmartctl(data.AtaDeviceStatistics, ataDeviceStats); err != nil {
			return nil
		}
	}
	for pageIdx := range ataDeviceStats.Pages {
		page := &ataDeviceStats.Pages[pageIdx]
		if page.Number != entryNumber {
			continue
		}
		for entryIdx := range page.Table {
			entry := &page.Table[entryIdx]
			if !strings.EqualFold(entry.Name, entryName) {
				continue
			}
			if entry.Value == nil || *entry.Value < minValue || *entry.Value > maxValue {
				return nil
			}
			return entry.Value
		}
	}
	return nil
}

func scsiAttributes(data *smartmodel.SmartctlOutput) []*smartmodel.SmartAttribute {
	var powerOn smartmodel.SmartPowerOnTime
	if data.PowerOnTime != nil {
		powerOn = *data.PowerOnTime
	}
	cycles := data.ScsiStartStopCycleCounter
	attributes := []*smartmodel.SmartAttribute{
		{Name: "PowerOnHours", RawValue: powerOn.Hours},
		{Name: "PowerOnMinutes", RawValue: powerOn.Minutes},
		{Name: "GrownDefectList", RawValue: data.ScsiGrownDefectList},
		{Name: "StartStopCycles", RawValue: cycles.AccumulatedStartStopCycles},
		{Name: "LoadUnloadCycles", RawValue: cycles.AccumulatedLoadUnloadCycles},
		{Name: "StartStopSpecified", RawValue: cycles.SpecifiedCycleCountOverDeviceLifetime},
		{Name: "LoadUnloadSpecified", RawValue: cycles.SpecifiedLoadUnloadCountOverDeviceLifetime},
	}

	var errorLog smartmodel.ScsiErrorCounterLog
	if data.ScsiErrorCounterLog != nil {
		errorLog = *data.ScsiErrorCounterLog
	}
	for _, counter := range []struct {
		prefix string
		stats  smartmodel.ScsiErrorCounter
	}{{"Read", errorLog.Read}, {"Write", errorLog.Write}, {"Verify", errorLog.Verify}} {
		attributes = append(attributes,
			&smartmodel.SmartAttribute{Name: counter.prefix + "TotalErrorsCorrected", RawValue: counter.stats.TotalErrorsCorrected},
			&smartmodel.SmartAttribute{Name: counter.prefix + "TotalUncorrectedErrors", RawValue: counter.stats.TotalUncorrectedErrors},
			&smartmodel.SmartAttribute{Name: counter.prefix + "CorrectionAlgorithmInvocations", RawValue: counter.stats.CorrectionAlgorithmInvocations},
		)
		if val := parseScsiGigabytesProcessed(counter.stats.GigabytesProcessed); val >= 0 {
			attributes = append(attributes, &smartmodel.SmartAttribute{Name: counter.prefix + "GigabytesProcessed", RawValue: uint64(val)})
		}
	}
	return attributes
}

func parseScsiGigabytesProcessed(value string) int64 {
	if value == "" {
		return -1
	}
	normalized := strings.ReplaceAll(value, ",", "")
	parsed, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}

// nvmeAttributes maps the NVMe health log, which does not follow the ATA
// attribute format, to SMART attributes.
func nvmeAttributes(log *smartmodel.SmartctlNVMeHealthLog, temperature uint8) []*smartmodel.SmartAttribute {
	return []*smartmodel.SmartAttribute{
		{Name: "CriticalWarning", RawValue: log.CriticalWarning},
		{Name: "Temperature", RawValue: uint64(temperature)},
		{Name: "AvailableSpare", RawValue: log.AvailableSpare},
		{Name: "AvailableSpareThreshold", RawValue: log.AvailableSpareThreshold},
		{Name: "PercentageUsed", RawValue: log.PercentageUsed},
		nvmeCounterAttribute("DataUnitsRead", log.DataUnitsRead),
		nvmeCounterAttribute("DataUnitsWritten", log.DataUnitsWritten),
		nvmeCounterAttribute("HostReads", log.HostReads),
		nvmeCounterAttribute("HostWrites", log.HostWrites),
		nvmeCounterAttribute("ControllerBusyTime", log.ControllerBusyTime),
		nvmeCounterAttribute("PowerCycles", log.PowerCycles),
		nvmeCounterAttribute("PowerOnHours", log.PowerOnHours),
		nvmeCounterAttribute("UnsafeShutdowns", log.UnsafeShutdowns),
		nvmeCounterAttribute("MediaErrors", log.MediaErrors),
		nvmeCounterAttribute("NumErrLogEntries", log.NumErrLogEntries),
		{Name: "WarningTempTime", RawValue: log.WarningTempTime},
		{Name: "CriticalCompTime", RawValue: log.CriticalCompTime},
	}
}

// nvmeCounterAttribute reports an overflowed 128-bit counter as invalid
// instead of a made-up number.
func nvmeCounterAttribute(name string, c smartmodel.NVMeCounter) *smartmodel.SmartAttribute {
	if c.Overflow {
		return &smartmodel.SmartAttribute{Name: name, RawString: smartmodel.NVMeCounterInvalid}
	}
	return &smartmodel.SmartAttribute{Name: name, RawValue: c.Value}
}

// nvmeHealthRecord copies the health log into the record the storage page
// reads. A log with an overflowed counter is left out, since the page would
// show it as a number: the page then lists the attributes, where the counter
// reads as invalid.
func nvmeHealthRecord(log *smartmodel.SmartctlNVMeHealthLog) *smartmodel.NVMeSmartHealthInformationLog {
	counters := []smartmodel.NVMeCounter{
		log.DataUnitsRead, log.DataUnitsWritten, log.HostReads, log.HostWrites,
		log.ControllerBusyTime, log.PowerCycles, log.PowerOnHours,
		log.UnsafeShutdowns, log.MediaErrors, log.NumErrLogEntries,
	}
	for _, counter := range counters {
		if counter.Overflow {
			return nil
		}
	}
	return &smartmodel.NVMeSmartHealthInformationLog{
		NSID:                    log.NSID,
		CriticalWarning:         log.CriticalWarning,
		Temperature:             log.Temperature,
		AvailableSpare:          log.AvailableSpare,
		AvailableSpareThreshold: log.AvailableSpareThreshold,
		PercentageUsed:          log.PercentageUsed,
		DataUnitsRead:           log.DataUnitsRead.Value,
		DataUnitsWritten:        log.DataUnitsWritten.Value,
		HostReads:               log.HostReads.Value,
		HostWrites:              log.HostWrites.Value,
		ControllerBusyTime:      log.ControllerBusyTime.Value,
		PowerCycles:             log.PowerCycles.Value,
		PowerOnHours:            log.PowerOnHours.Value,
		UnsafeShutdowns:         log.UnsafeShutdowns.Value,
		MediaErrors:             log.MediaErrors.Value,
		NumErrLogEntries:        log.NumErrLogEntries.Value,
		WarningTempTime:         log.WarningTempTime,
		CriticalCompTime:        log.CriticalCompTime,
		TemperatureSensors:      log.TemperatureSensors,
	}
}
