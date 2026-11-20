package storage

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// validDeviceNameRe limits SMART self-tests to whole disks.
var validDeviceNameRe = regexp.MustCompile(`^(sd[a-z]|hd[a-z]|nvme\d+n\d+)$`)

// RunSmartTest starts a SMART self-test on the specified device.
// testType can be "short" or "long" (extended).
func RunSmartTest(ctx context.Context, device, testType string) error {
	if !validDeviceNameRe.MatchString(device) {
		return errors.New("invalid device name")
	}

	// Validate test type
	var smartTestArg string
	switch testType {
	case "short":
		smartTestArg = "short"
	case "long":
		smartTestArg = "long"
	default:
		return fmt.Errorf("invalid test type: %s (use 'short' or 'long')", testType)
	}

	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return fmt.Errorf("smartctl not found: %w", err)
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
			return fmt.Errorf("smartctl self-test failed for %s: %w\nOutput: %s", device, err, outStr)
		}
	}

	return nil
}

// abortSmartTest aborts the self-test device runs (smartctl -X). It is a
// test seam.
var abortSmartTest = func(ctx context.Context, device string) error {
	if !validDeviceNameRe.MatchString(device) {
		return errors.New("invalid device name")
	}
	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return fmt.Errorf("smartctl not found: %w", err)
	}
	out, err := exec.CommandContext(ctx, smartctlPath, "-X", "/dev/"+device).CombinedOutput()
	// Its exit status is a bitmask that can be non-zero after an abort.
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "aborted") {
		return fmt.Errorf("smartctl self-test abort failed for %s: %w\nOutput: %s", device, err, out)
	}
	return nil
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
	ModelName    string           `json:"model_name"`
	SerialNumber string           `json:"serial_number"`
	WWN          *smartctlWWN     `json:"wwn"`
	ATA          *ataSmartData    `json:"ata_smart_data"`
	ATALog       *ataSelfTestLog  `json:"ata_smart_self_test_log"`
	NVMe         *nvmeSelfTestLog `json:"nvme_self_test_log"`
}

type smartctlWWN struct {
	NAA uint64 `json:"naa"`
	OUI uint64 `json:"oui"`
	ID  uint64 `json:"id"`
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

// ataSelfTestLog is the drive's standard self-test log, newest first.
type ataSelfTestLog struct {
	Standard *struct {
		Table []ataSelfTestLogEntry `json:"table"`
	} `json:"standard"`
}

type ataSelfTestLogEntry struct {
	Type          *intOrObj          `json:"type"`
	Status        *ataSelfTestStatus `json:"status"`
	LifetimeHours int                `json:"lifetime_hours"`
}

// The self-test execution status of an ATA drive is the high nibble of its
// status byte.
const (
	ataSelfTestCompleted   = 0x0
	ataSelfTestAborted     = 0x1 // by the host, as smartctl -X does
	ataSelfTestInterrupted = 0x2 // by a hard or soft reset of the drive
	ataSelfTestInProgress  = 0xf
)

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
	SelfTestCode   *nvmeSelfTestResult `json:"self_test_code"`
	SelfTestResult *nvmeSelfTestResult `json:"self_test_result"`
	PowerOnHours   int                 `json:"power_on_hours"`
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
	if err := json.Unmarshal(b, &n, json.MatchCaseInsensitiveNames(true)); err == nil {
		i.Value = n
		return nil
	}
	// Fall back to object { "value": N }.
	var obj struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(b, &obj, json.MatchCaseInsensitiveNames(true)); err != nil {
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
	if err := json.Unmarshal(b, &out, json.MatchCaseInsensitiveNames(true)); err != nil {
		return SmartTestStatus{}, fmt.Errorf("parse smartctl json: %w", err)
	}
	return out.status(), nil
}

func (out smartctlOutput) status() SmartTestStatus {
	if st, ok := parseATASmartTestStatus(out.ATA); ok {
		return st
	}
	if out.NVMe != nil {
		return parseNVMeSmartTestStatus(out.NVMe)
	}

	// Neither block present.
	return SmartTestStatus{State: "idle"}
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
		// smartctl leaves passed out for a test the host aborted or a reset
		// interrupted: neither passed.
		if st.Value != nil && (*st.Value>>4 == ataSelfTestAborted || *st.Value>>4 == ataSelfTestInterrupted) {
			return SmartTestStatus{State: "aborted", Message: st.String}, true
		}
		// Status block present but no actionable fields -> idle.
		return SmartTestStatus{State: "idle", Message: st.String}, true
	}
	if *st.Passed {
		return SmartTestStatus{State: "completed", PercentComplete: 100, Message: st.String}, true
	}
	return SmartTestStatus{State: "failed", PercentComplete: 0, Message: st.String}, true
}

func parseNVMeSmartTestStatus(testLog *nvmeSelfTestLog) SmartTestStatus {
	if opCode, ok := firstIntOrObjValue(testLog.CurrentSelfTestOp, testLog.CurrentSelfTestOperation); ok && opCode != 0 {
		return SmartTestStatus{
			State:           "in_progress",
			PercentComplete: nvmeCompletionPercent(testLog),
		}
	}
	if result, ok := latestNVMeSelfTestResult(testLog); ok {
		return nvmeResultCodeToStatus(*result.Value, result.String)
	}
	// NVMe block present but no actionable info -> idle.
	return SmartTestStatus{State: "idle"}
}

func nvmeCompletionPercent(testLog *nvmeSelfTestLog) int {
	pct, ok := firstIntOrObjValue(testLog.CurrentSelfTestCompletionPercent, testLog.CurrentSelfTestCompletion)
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

func latestNVMeSelfTestResult(testLog *nvmeSelfTestLog) (*nvmeSelfTestResult, bool) {
	if len(testLog.Table) == 0 || testLog.Table[0].SelfTestResult == nil ||
		testLog.Table[0].SelfTestResult.Value == nil {
		return nil, false
	}
	return testLog.Table[0].SelfTestResult, true
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

// smartctlJSON is a test seam: `smartctl --json -a /dev/X`, its stdout and
// its run error. Uses Output() so stderr can't poison stdout JSON.
var smartctlJSON = func(ctx context.Context, device string) ([]byte, error) {
	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, fmt.Errorf("smartctl not found: %w", err)
	}
	return exec.CommandContext(ctx, smartctlPath, "--json", "-a", "/dev/"+device).Output()
}

// PollSmartTestStatus runs `smartctl --json -a /dev/X` and returns the parsed
// SMART self-test state.
func PollSmartTestStatus(ctx context.Context, device string) (SmartTestStatus, error) {
	if !validDeviceNameRe.MatchString(device) {
		return SmartTestStatus{}, errors.New("invalid device name")
	}
	out, runErr := smartctlJSON(ctx, device)
	return interpretSmartctlResult(out, runErr)
}

// smartIdentity is who a drive is, as smartctl reads it from the drive
// itself, whatever /dev name it has.
type smartIdentity struct {
	Model  string `json:"model,omitzero"`
	Serial string `json:"serial,omitzero"`
	WWN    string `json:"wwn,omitzero"`
}

// selfTestEntry is one test of a drive's self-test log, which the drive keeps
// across reboots.
type selfTestEntry struct {
	// Kind is "short", "long" or "other".
	Kind string `json:"kind"`
	// Result is "completed", "aborted" (by the host, or another command),
	// "interrupted" (by a reset of the drive), "failed", or "in_progress" for
	// the test an ATA drive may log while it runs it.
	Result string `json:"result"`
	// Hours is the drive's power-on hours when the test ended.
	Hours int `json:"hours"`
	// Text is the drive's words for the result.
	Text string `json:"text,omitzero"`
}

// smartDrive is what a self-test job reads of a drive.
type smartDrive struct {
	identity smartIdentity
	status   SmartTestStatus
	// running is the kind of self-test the drive runs, "short" or "long", or
	// "" when it runs none or does not say, as an ATA drive does not.
	running string
	// log is the drive's self-test log, newest first.
	log []selfTestEntry
}

// runningKind is the kind of self-test the drive says it runs, from its
// status (NVMe) or the test it logs as running (some ATA drives); "" when it
// does not say.
func (d smartDrive) runningKind() string {
	if d.running != "" {
		return d.running
	}
	for _, entry := range d.log {
		if entry.Result == "in_progress" {
			return entry.Kind
		}
	}
	return ""
}

// readSmartDrive reads who device is, its self-test status and its self-test
// log.
func readSmartDrive(ctx context.Context, device string) (smartDrive, error) {
	if !validDeviceNameRe.MatchString(device) {
		return smartDrive{}, errors.New("invalid device name")
	}
	raw, runErr := smartctlJSON(ctx, device)
	var out smartctlOutput
	if len(raw) == 0 || json.Unmarshal(raw, &out, json.MatchCaseInsensitiveNames(true)) != nil {
		_, err := interpretSmartctlResult(raw, runErr)
		return smartDrive{}, err
	}
	drive := smartDrive{
		identity: smartIdentity{Model: strings.TrimSpace(out.ModelName), Serial: strings.TrimSpace(out.SerialNumber)},
		status:   out.status(),
	}
	if out.WWN != nil {
		drive.identity.WWN = fmt.Sprintf("%x-%x-%x", out.WWN.NAA, out.WWN.OUI, out.WWN.ID)
	}
	if out.ATALog != nil && out.ATALog.Standard != nil {
		for _, entry := range out.ATALog.Standard.Table {
			if logged, ok := ataSelfTestEntry(entry); ok {
				drive.log = append(drive.log, logged)
			}
		}
	}
	if out.NVMe != nil {
		switch op, _ := firstIntOrObjValue(out.NVMe.CurrentSelfTestOp, out.NVMe.CurrentSelfTestOperation); op {
		case 1:
			drive.running = "short"
		case 2:
			drive.running = "long"
		}
		for _, entry := range out.NVMe.Table {
			if logged, ok := nvmeSelfTestEntry(entry); ok {
				drive.log = append(drive.log, logged)
			}
		}
	}
	return drive, nil
}

func ataSelfTestEntry(entry ataSelfTestLogEntry) (selfTestEntry, bool) {
	if entry.Type == nil || entry.Status == nil || entry.Status.Value == nil {
		return selfTestEntry{}, false
	}
	logged := selfTestEntry{Hours: entry.LifetimeHours, Text: entry.Status.String}
	// Offline (1, 2) and captive (129, 130) short and extended tests.
	switch entry.Type.Value {
	case 1, 129:
		logged.Kind = "short"
	case 2, 130:
		logged.Kind = "long"
	default:
		logged.Kind = "other"
	}
	switch *entry.Status.Value >> 4 {
	case ataSelfTestCompleted:
		logged.Result = "completed"
	case ataSelfTestAborted:
		logged.Result = "aborted"
	case ataSelfTestInterrupted:
		logged.Result = "interrupted"
	case ataSelfTestInProgress:
		logged.Result = "in_progress"
	default:
		logged.Result = "failed"
	}
	return logged, true
}

func nvmeSelfTestEntry(entry nvmeSelfTestLogEntry) (selfTestEntry, bool) {
	if entry.SelfTestCode == nil || entry.SelfTestCode.Value == nil || entry.SelfTestResult == nil || entry.SelfTestResult.Value == nil {
		return selfTestEntry{}, false
	}
	logged := selfTestEntry{Hours: entry.PowerOnHours, Text: entry.SelfTestResult.String}
	switch *entry.SelfTestCode.Value {
	case 1:
		logged.Kind = "short"
	case 2:
		logged.Kind = "long"
	default:
		logged.Kind = "other"
	}
	// The NVMe self-test result codes; 2 is a controller reset, 15 an
	// unused entry.
	switch code := *entry.SelfTestResult.Value; code {
	case 15:
		return selfTestEntry{}, false
	case 2:
		logged.Result = "interrupted"
	default:
		logged.Result = nvmeResultCodeToStatus(code, "").State
	}
	return logged, true
}
