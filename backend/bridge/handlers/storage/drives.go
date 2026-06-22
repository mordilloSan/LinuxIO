package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type LSBLKOutput struct {
	BlockDevices []BlockDevice `json:"blockdevices"`
}

type BlockDevice struct {
	Name       string        `json:"name"`
	Model      string        `json:"model"`
	Serial     string        `json:"serial"`
	Size       string        `json:"size"`
	RO         bool          `json:"ro"`
	Type       string        `json:"type"`
	Tran       string        `json:"tran"`
	Vendor     string        `json:"vendor"`
	Mountpoint string        `json:"mountpoint"`
	Children   []BlockDevice `json:"children,omitempty"`
}

// precompiled regexes
var (
	validDeviceNameRe = regexp.MustCompile(`^(sd[a-z]|hd[a-z]|nvme\d+n\d+)$`)
	nvmePsRe          = regexp.MustCompile(`ps\s+(\d+)\s+:\s+mp:([\d.]+)W`)
	nvmeStateRe       = regexp.MustCompile(`Power State:\s+(\d+)`)
)

func FetchDriveInfo(ctx context.Context) ([]DriveInfo, error) {
	out, err := exec.CommandContext(ctx, "lsblk", "-d", "-O", "-J").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute lsblk: %w", err)
	}

	var parsed LSBLKOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse lsblk output: %w", err)
	}

	drives := make([]DriveInfo, 0, len(parsed.BlockDevices))
	for _, dev := range parsed.BlockDevices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if dev.Type != "disk" {
			continue
		}
		drives = append(drives, buildDriveInfo(ctx, dev))
	}

	return drives, nil
}

func buildDriveInfo(ctx context.Context, dev BlockDevice) DriveInfo {
	drive := DriveInfo{
		Name:   dev.Name,
		Model:  strings.TrimSpace(dev.Model),
		Serial: optionalString(strings.TrimSpace(dev.Serial)),
		Size:   dev.Size,
		Type:   optionalString(dev.Tran),
		Vendor: optionalString(strings.TrimSpace(dev.Vendor)),
		RO:     dev.RO,
	}

	if smart, err := FetchSmartInfo(ctx, dev.Name); err != nil {
		drive.SmartError = err.Error()
	} else {
		drive.Smart = smart
		if drive.Vendor == nil {
			if smart.ModelName != "" {
				if parts := strings.Fields(smart.ModelName); len(parts) > 0 {
					drive.Vendor = optionalString(parts[0])
				}
			}
		}
	}

	if isNVMeDevice(dev) {
		if power, err := GetNVMePowerState(ctx, dev.Name); err != nil {
			drive.PowerError = err.Error()
		} else {
			drive.Power = power
		}
	}

	return drive
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func isNVMeDevice(dev BlockDevice) bool {
	// On some systems lsblk tran for NVMe may not be "nvme", so also check the name.
	if strings.HasPrefix(dev.Name, "nvme") {
		return true
	}
	return dev.Tran == "nvme"
}

func FetchSmartInfo(ctx context.Context, device string) (*apischema.SmartData, error) {
	if !validDeviceNameRe.MatchString(device) {
		return nil, errors.New("invalid device name")
	}

	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, fmt.Errorf("smartctl not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, smartctlPath, "--json", "-x", "/dev/"+device)
	out, err := cmd.Output()
	if err != nil {
		// smartctl returns non-zero if drive doesn't support SMART, etc.
		// We still try to use whatever JSON it produced, but wrap the error.
		return nil, fmt.Errorf("smartctl failed for %s: %w", device, err)
	}

	var parsed apischema.SmartData
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse smartctl output: %w", err)
	}

	return &parsed, nil
}

func GetNVMePowerState(ctx context.Context, device string) (*apischema.DiskPowerData, error) {
	// Step 1: Get supported power states
	cmd := exec.CommandContext(ctx, "nvme", "id-ctrl", "/dev/"+device)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvme id-ctrl for %s: %w", device, err)
	}

	var states []apischema.DiskPowerState
	for line := range strings.SplitSeq(string(out), "\n") {
		match := nvmePsRe.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}

		stateNum, parseErr := strconv.Atoi(match[1])
		if parseErr != nil {
			continue
		}
		maxPower, powerErr := strconv.ParseFloat(match[2], 64)
		if powerErr != nil {
			continue
		}

		states = append(states, apischema.DiskPowerState{
			State:       stateNum,
			MaxPowerW:   maxPower,
			Description: strings.TrimSpace(line),
		})
	}

	if len(states) == 0 {
		return nil, fmt.Errorf("no power states found for %s", device)
	}

	currentState, estimated := resolveCurrentNVMePowerState(ctx, device, states)

	return &apischema.DiskPowerData{
		CurrentState: currentState,
		EstimatedW:   estimated,
		States:       states,
	}, nil
}

func resolveCurrentNVMePowerState(ctx context.Context, device string, states []apischema.DiskPowerState) (int, float64) {
	cmd := exec.CommandContext(ctx, "nvme", "smart-log", "/dev/"+device)
	out, err := cmd.Output()
	if err != nil {
		if len(states) > 0 {
			return -1, states[0].MaxPowerW
		}
		return -1, 0
	}

	match := nvmeStateRe.FindStringSubmatch(string(out))
	if len(match) == 2 {
		if s, err := strconv.Atoi(match[1]); err == nil {
			for _, ps := range states {
				if ps.State == s {
					return s, ps.MaxPowerW
				}
			}
			return s, 0
		}
	}

	if len(states) > 0 {
		return -1, states[0].MaxPowerW
	}
	return -1, 0
}

// RunSmartTest starts a SMART self-test on the specified device.
// testType can be "short" or "long" (extended).
func RunSmartTest(ctx context.Context, device, testType string) (map[string]any, error) {
	if !validDeviceNameRe.MatchString(device) {
		return nil, errors.New("invalid device name")
	}

	// Validate test type
	var smartTestArg string
	switch testType {
	case "short":
		smartTestArg = "short"
	case "long":
		smartTestArg = "long"
	default:
		return nil, fmt.Errorf("invalid test type: %s (use 'short' or 'long')", testType)
	}

	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, fmt.Errorf("smartctl not found: %w", err)
	}

	// Run the self-test
	cmd := exec.CommandContext(ctx, smartctlPath, "-t", smartTestArg, "/dev/"+device)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// smartctl may return non-zero even on success for some operations
		// Check if the output contains success indicators
		outStr := string(out)
		if !strings.Contains(outStr, "Testing has begun") &&
			!strings.Contains(outStr, "Self-test routine") {
			return nil, fmt.Errorf("smartctl self-test failed for %s: %w\nOutput: %s", device, err, outStr)
		}
	}

	return map[string]any{
		"success": true,
		"device":  device,
		"test":    testType,
		"message": fmt.Sprintf("SMART %s self-test started on /dev/%s", testType, device),
		"output":  string(out),
	}, nil
}

// SmartTestStatus represents the current state of a SMART self-test on a drive.
type SmartTestStatus struct {
	State           string // "in_progress" | "completed" | "failed" | "aborted" | "idle"
	PercentComplete int    // 0..100
	Message         string
}

// smartctlOutput is the subset of `smartctl --json -a /dev/X` output we care about.
// NVMe schemas vary across smartmontools versions: current versions emit
// current_self_test_operation as an object with .value, paired with the scalar
// current_self_test_completion_percent. Older / fully-scalar / fully-object
// variants are also supported via custom unmarshalling below.
type smartctlOutput struct {
	ATA  *ataSmartData    `json:"ata_smart_data"`
	NVMe *nvmeSelfTestLog `json:"nvme_self_test_log"`
}

type ataSmartData struct {
	SelfTest *ataSelfTest `json:"self_test"`
}

type ataSelfTest struct {
	Status *ataSelfTestStatus `json:"status"`
}

type ataSelfTestStatus struct {
	Value            *int   `json:"value"`
	String           string `json:"string"`
	Passed           *bool  `json:"passed"`
	RemainingPercent *int   `json:"remaining_percent"`
}

type nvmeSelfTestLog struct {
	// Newer smartctl: scalar.
	CurrentSelfTestOp                *intOrObj `json:"current_self_test_op"`
	CurrentSelfTestCompletionPercent *intOrObj `json:"current_self_test_completion_percent"`
	// Older / object form.
	CurrentSelfTestOperation  *intOrObj              `json:"current_self_test_operation"`
	CurrentSelfTestCompletion *intOrObj              `json:"current_self_test_completion"`
	Table                     []nvmeSelfTestLogEntry `json:"table"`
}

type nvmeSelfTestLogEntry struct {
	SelfTestResult *nvmeSelfTestResult `json:"self_test_result"`
}

type nvmeSelfTestResult struct {
	Value  *int   `json:"value"`
	String string `json:"string"`
}

// intOrObj decodes either a JSON number or an object with a `value` field.
// Some smartmontools versions emit `current_self_test_operation: { value: 1 }`
// while others emit `current_self_test_op: 1` as a plain integer.
type intOrObj struct {
	Value int
}

func (i *intOrObj) UnmarshalJSON(b []byte) error {
	// Try plain int first.
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		i.Value = n
		return nil
	}
	// Fall back to object { "value": N }.
	var obj struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	i.Value = obj.Value
	return nil
}

// parseSmartTestJSON converts a smartctl JSON blob into a SmartTestStatus.
// Pure function — exported indirectly via interpretSmartctlResult so tests
// can drive it with canned bytes.
func parseSmartTestJSON(b []byte) (SmartTestStatus, error) {
	var out smartctlOutput
	if err := json.Unmarshal(b, &out); err != nil {
		return SmartTestStatus{}, fmt.Errorf("parse smartctl json: %w", err)
	}

	if st, ok := parseATASmartTestStatus(out.ATA); ok {
		return st, nil
	}
	if out.NVMe != nil {
		return parseNVMeSmartTestStatus(out.NVMe), nil
	}

	// Neither block present.
	return SmartTestStatus{State: "idle"}, nil
}

func parseATASmartTestStatus(ata *ataSmartData) (SmartTestStatus, bool) {
	if ata == nil || ata.SelfTest == nil || ata.SelfTest.Status == nil {
		return SmartTestStatus{}, false
	}

	st := ata.SelfTest.Status
	// Prefer documented fields. `remaining_percent` is present only while in progress.
	if st.RemainingPercent != nil {
		rem := min(max(*st.RemainingPercent, 0), 100)
		return SmartTestStatus{
			State:           "in_progress",
			PercentComplete: 100 - rem,
			Message:         st.String,
		}, true
	}
	if st.Passed == nil {
		// Status block present but no actionable fields -> idle.
		return SmartTestStatus{State: "idle", Message: st.String}, true
	}
	if *st.Passed {
		return SmartTestStatus{State: "completed", PercentComplete: 100, Message: st.String}, true
	}
	return SmartTestStatus{State: "failed", PercentComplete: 0, Message: st.String}, true
}

func parseNVMeSmartTestStatus(log *nvmeSelfTestLog) SmartTestStatus {
	if opCode, ok := firstIntOrObjValue(log.CurrentSelfTestOp, log.CurrentSelfTestOperation); ok && opCode != 0 {
		return SmartTestStatus{
			State:           "in_progress",
			PercentComplete: nvmeCompletionPercent(log),
		}
	}
	if result, ok := latestNVMeSelfTestResult(log); ok {
		return nvmeResultCodeToStatus(*result.Value, result.String)
	}
	// NVMe block present but no actionable info -> idle.
	return SmartTestStatus{State: "idle"}
}

func nvmeCompletionPercent(log *nvmeSelfTestLog) int {
	pct, ok := firstIntOrObjValue(log.CurrentSelfTestCompletionPercent, log.CurrentSelfTestCompletion)
	if !ok {
		return 0
	}
	return min(max(pct, 0), 100)
}

func firstIntOrObjValue(values ...*intOrObj) (int, bool) {
	for _, value := range values {
		if value != nil {
			return value.Value, true
		}
	}
	return 0, false
}

func latestNVMeSelfTestResult(log *nvmeSelfTestLog) (*nvmeSelfTestResult, bool) {
	if len(log.Table) == 0 || log.Table[0].SelfTestResult == nil ||
		log.Table[0].SelfTestResult.Value == nil {
		return nil, false
	}
	return log.Table[0].SelfTestResult, true
}

// nvmeResultCodeToStatus maps the NVMe self-test result code to a status.
// Mapping matches current smartmontools (nvmeprint.cpp) and the NVMe spec:
//   - 0:           passed
//   - 1, 2, 3, 4, 8, 9: aborted (operation interrupted, not a real failure)
//   - 5, 6, 7:     failed (fatal error or segment failure)
func nvmeResultCodeToStatus(code int, msg string) SmartTestStatus {
	switch code {
	case 0:
		return SmartTestStatus{State: "completed", PercentComplete: 100, Message: msg}
	case 1, 2, 3, 4, 8, 9:
		return SmartTestStatus{State: "aborted", Message: msg}
	case 5, 6, 7:
		return SmartTestStatus{State: "failed", Message: msg}
	default:
		return SmartTestStatus{State: "failed", Message: msg}
	}
}

// interpretSmartctlResult parses smartctl output, tolerating non-zero exit
// codes when the JSON itself is intact. smartctl encodes many non-fatal
// conditions in its exit status (bitmask), so a non-zero exit alongside
// valid JSON is normal.
func interpretSmartctlResult(out []byte, runErr error) (SmartTestStatus, error) {
	if len(out) > 0 {
		if st, err := parseSmartTestJSON(out); err == nil {
			return st, nil
		}
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && len(exitErr.Stderr) > 0 {
			return SmartTestStatus{}, fmt.Errorf("smartctl failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return SmartTestStatus{}, fmt.Errorf("smartctl failed: %w", runErr)
	}
	return SmartTestStatus{}, errors.New("smartctl produced no parseable output")
}

// PollSmartTestStatus runs `smartctl --json -a /dev/X` and returns the parsed
// SMART self-test state. Uses cmd.Output() so stderr can't poison stdout JSON.
func PollSmartTestStatus(ctx context.Context, device string) (SmartTestStatus, error) {
	if !validDeviceNameRe.MatchString(device) {
		return SmartTestStatus{}, errors.New("invalid device name")
	}
	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return SmartTestStatus{}, fmt.Errorf("smartctl not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, smartctlPath, "--json", "-a", "/dev/"+device)
	out, runErr := cmd.Output()
	return interpretSmartctlResult(out, runErr)
}
