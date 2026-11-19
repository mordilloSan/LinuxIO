package storage

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

type SmartTestProgress struct {
	Type       string `json:"type"`
	Device     string `json:"device,omitempty"`
	TestType   string `json:"test_type,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	Percentage *int   `json:"percentage,omitzero"`
}

func (p SmartTestProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	return bridgeipc.TaskProgress{
		Percentage: p.Percentage,
		Phase:      p.Status,
		Message:    p.Message,
		Detail:     p,
	}
}

type SmartTestResult struct {
	Success  bool   `json:"success"`
	Device   string `json:"device"`
	Test     string `json:"test"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Duration *int64 `json:"duration"`
}

func smartTestBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.MutationTask("storage.run_smart_test", runSmartTestTask, apischema.Privileged(), apischema.Progress[SmartTestProgress](), apischema.WithTaskMetadata(func(req apischema.DeviceTestTypeRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Device, req.TestType}, Label: "Running SMART self-test", Device: req.Device, TestType: req.TestType}
		})).Resumable(apischema.ResumeHooks[apischema.DeviceTestTypeRequest, SmartTestResult]{
			Suspend: suspendSmartTest,
			Resume:  resumeSmartTest,
			// The test runs in the drive, which outlives the job host and
			// remembers its result across a reboot.
			At: bridgeipc.ResumeInBoot | bridgeipc.ResumeAfterReboot,
		}),
	)
}

// pollInterval picks how often to poll smartctl based on test type. Short tests
// finish in ~2 minutes so a tighter loop gives smoother progress; long tests
// can run for hours and don't benefit from frequent polling. It is a test
// seam.
var pollInterval = func(testType string) time.Duration {
	if testType == "long" {
		return 60 * time.Second
	}
	return 15 * time.Second
}

// smartTestStallTimeout fails a self-test whose reported progress has not moved
// for this long. It is not a total cap: extended tests on large disks run for
// 8-30 h, and ATA reports progress in 10% steps (up to ~3 h each).
// smartTerminalPolls is how many successful polls must pass before a terminal
// state counts without in_progress ever having been seen (it may have finished
// between polls, or never started).
const (
	smartTestStallTimeout = 6 * time.Hour
	smartTerminalPolls    = 3
)

var errSmartTestTimeout = errors.New("SMART self-test did not finish within the time limit")

// Test seams.
var (
	pollSmartStatus     = PollSmartTestStatus
	startSmartTest      = RunSmartTest
	saveSmartCheckpoint = bridgeipc.SaveCheckpoint
)

// smartCheckpoint is what a self-test job read of its drive before it started
// the test, saved in its record: who the drive is, the kind of test and the
// drive's self-test log then. A pick-up after the job's host died, or after a
// reboot, takes the drive and a test for the job's only when they match it.
type smartCheckpoint struct {
	Test     string          `json:"test"`
	Identity smartIdentity   `json:"identity"`
	Log      []selfTestEntry `json:"log,omitzero"`
	// Started is saved once smartctl started the test: without it the test
	// the drive runs or logged may be another program's.
	Started bool `json:"started,omitzero"`
}

func runSmartTestTask(ctx context.Context, task *bridgeipc.Task, req apischema.DeviceTestTypeRequest) (SmartTestResult, error) {
	state := smartTestTaskState{
		task:     task,
		device:   req.Device,
		testType: req.TestType,
		interval: pollInterval(req.TestType),
		timeout:  smartTestStallTimeout,
	}
	state.reportStart()

	drive, err := readSmartDrive(ctx, req.Device)
	if err != nil {
		return SmartTestResult{}, fmt.Errorf("read /dev/%s before the self-test: %w", req.Device, err)
	}
	cp := smartCheckpoint{Test: req.TestType, Identity: drive.identity, Log: drive.log}
	if err = saveSmartCheckpoint(ctx, cp); err != nil {
		return SmartTestResult{}, fmt.Errorf("save the state of /dev/%s before the self-test: %w", req.Device, err)
	}
	if err = ctx.Err(); err != nil {
		return SmartTestResult{}, err
	}
	if err = startSmartTest(ctx, req.Device, req.TestType); err != nil {
		// Refused, as beside a test smartd or an admin started: nothing of
		// the job's runs to abort.
		return SmartTestResult{}, err
	}
	defer abortIfCanceled(ctx, req.Device, cp.Identity, req.TestType)
	cp.Started = true
	if err = saveSmartCheckpoint(ctx, cp); err != nil {
		// The job follows the test all the same; only a pick-up loses it.
		slog.Warn("SMART self-test start not recorded", "device", req.Device, "error", err)
	}

	state.pollInitial(ctx)
	return state.pollUntilDone(ctx)
}

// smartAbortTimeout bounds the abort of a canceled self-test.
const smartAbortTimeout = 30 * time.Second

// abortIfCanceled aborts the drive's self-test when the job's owner canceled
// the job, device is still the drive the job tested and it runs a test, of
// the job's kind where the drive says the kind. When its host stops the job
// instead, the test runs on in the drive, for the next host to pick the job
// up.
func abortIfCanceled(ctx context.Context, device string, identity smartIdentity, testType string) {
	if ctx.Err() == nil {
		return
	}
	if task, ok := bridgeipc.TaskFromContext(ctx); ok && task.Interrupted() {
		return
	}
	if identity.Serial == "" && identity.WWN == "" {
		slog.Warn("SMART self-test not aborted", "device", device, "error", errors.New("the drive reports no serial number or WWN to know it again by"))
		return
	}
	abortCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), smartAbortTimeout)
	defer stop()
	drive, err := readSmartDrive(abortCtx, device)
	switch {
	case err != nil:
		slog.Warn("SMART self-test not aborted", "device", device, "error", err)
	case drive.identity != identity:
		slog.Warn("SMART self-test not aborted", "device", device, "error", errors.New("another drive has its name now"))
	case drive.status.State != "in_progress":
		slog.Debug("SMART self-test ended before its cancel", "device", device)
	case drive.runningKind() != "" && drive.runningKind() != testType:
		slog.Debug("SMART self-test of another kind left running", "device", device, "running", drive.runningKind())
	default:
		if err = abortSmartTest(abortCtx, device); err != nil {
			slog.Warn("SMART self-test not aborted", "device", device, "error", err)
			return
		}
		slog.Info("SMART self-test aborted", "device", device)
	}
}

// suspendSmartTest keeps the checkpoint the job saved before it started the
// test, which runs on in the drive.
func suspendSmartTest(context.Context, apischema.DeviceTestTypeRequest, jsontext.Value) (jsontext.Value, error) {
	return nil, nil
}

// resumeSmartTest picks a self-test job up again from the drive's own state,
// at the root host's next start after its host died or was stopped, or after
// a reboot. It follows a test the drive still runs, or reads how the test
// ended from the drive's self-test log, and never starts one: when the drive
// cannot say what became of the job's test, or is another drive now, the job
// stays interrupted.
func resumeSmartTest(ctx context.Context, req apischema.DeviceTestTypeRequest, checkpoint jsontext.Value) (SmartTestResult, error) {
	cp, err := decodeSmartCheckpoint(checkpoint, req.TestType)
	if err != nil {
		return SmartTestResult{}, err
	}
	drive, err := readSmartDrive(ctx, req.Device)
	if err != nil {
		if ctx.Err() != nil {
			return SmartTestResult{}, ctx.Err()
		}
		return SmartTestResult{}, fmt.Errorf("%w: /dev/%s could not be read: %v", bridgeipc.ErrNotResumed, req.Device, err)
	}
	ended, err := smartPickUp(cp, drive, req.Device)
	if err != nil {
		return SmartTestResult{}, err
	}
	if ended != nil {
		return endedSelfTest(*ended, req.Device, req.TestType)
	}

	task, _ := bridgeipc.TaskFromContext(ctx) // Resume always runs as a task
	state := smartTestTaskState{
		task:     task,
		device:   req.Device,
		testType: req.TestType,
		interval: pollInterval(req.TestType),
		timeout:  smartTestStallTimeout,
		// The test was seen running, so its end is trusted at once.
		polls:          smartTerminalPolls,
		seenInProgress: true,
	}
	defer abortIfCanceled(ctx, req.Device, cp.Identity, req.TestType)
	state.emit(drive.status)
	return state.pollUntilDone(ctx)
}

func decodeSmartCheckpoint(raw jsontext.Value, testType string) (smartCheckpoint, error) {
	if len(raw) == 0 {
		return smartCheckpoint{}, fmt.Errorf("%w: its job host stopped before it read the drive", bridgeipc.ErrNotResumed)
	}
	var cp smartCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return smartCheckpoint{}, fmt.Errorf("%w: its checkpoint is unreadable: %v", bridgeipc.ErrNotResumed, err)
	}
	if cp.Test != testType {
		return smartCheckpoint{}, fmt.Errorf("%w: its checkpoint is of another test", bridgeipc.ErrNotResumed)
	}
	return cp, nil
}

// smartPickUp decides what a pick-up does with device as read now, drive,
// against the checkpoint the job saved before it started its test: follow the
// test the drive runs (nil, nil), take the ended test it returns as the job's,
// or leave the job interrupted (bridgeipc.ErrNotResumed).
func smartPickUp(cp smartCheckpoint, drive smartDrive, device string) (*selfTestEntry, error) {
	switch {
	case cp.Identity.Serial == "" && cp.Identity.WWN == "":
		return nil, fmt.Errorf("%w: /dev/%s reported no serial number or WWN to know it again by", bridgeipc.ErrNotResumed, device)
	case drive.identity != cp.Identity:
		return nil, fmt.Errorf("%w: /dev/%s is another drive now", bridgeipc.ErrNotResumed, device)
	case !cp.Started:
		return nil, fmt.Errorf("%w: its job host stopped before it started the self-test on /dev/%s", bridgeipc.ErrNotResumed, device)
	}
	added, ok := newSelfTests(cp.Log, drive.log)
	if !ok {
		return nil, fmt.Errorf("%w: the self-test log of /dev/%s does not continue the one read before the test", bridgeipc.ErrNotResumed, device)
	}
	// The test an ATA drive logs while it runs it is no result, but says
	// which kind runs.
	running := drive.running
	var ended []selfTestEntry
	for _, entry := range added {
		if entry.Result == "in_progress" {
			running = cmp.Or(running, entry.Kind)
			continue
		}
		ended = append(ended, entry)
	}
	if drive.status.State == "in_progress" && len(ended) == 0 {
		if running != "" && running != cp.Test {
			return nil, fmt.Errorf("%w: /dev/%s runs a %s self-test, not the job's %s one", bridgeipc.ErrNotResumed, device, running, cp.Test)
		}
		return nil, nil
	}
	switch {
	case len(ended) == 0:
		return nil, fmt.Errorf("%w: /dev/%s has no result of the self-test", bridgeipc.ErrNotResumed, device)
	case len(ended) > 1:
		return nil, fmt.Errorf("%w: /dev/%s ran %d self-tests since the job's started", bridgeipc.ErrNotResumed, device, len(ended))
	case ended[0].Kind != cp.Test:
		return nil, fmt.Errorf("%w: the newest self-test of /dev/%s is not a %s one", bridgeipc.ErrNotResumed, device, cp.Test)
	}
	return &ended[0], nil
}

// newSelfTests returns the tests of cur, a drive's self-test log now, that
// base, its log before the job's test, did not hold, newest first. The log is
// a ring (21 tests on ATA, 20 on NVMe) whose oldest fall out as new ones come
// in. ok is false when cur does not continue base.
func newSelfTests(base, cur []selfTestEntry) ([]selfTestEntry, bool) {
	if len(cur) > len(base) {
		added := len(cur) - len(base)
		return cur[:added], slices.Equal(cur[added:], base)
	}
	if len(cur) < len(base) {
		return nil, false
	}
	// A full ring, or nothing new: the new tests are those before the old
	// ones start.
	for added := range len(cur) {
		if slices.Equal(cur[added:], base[:len(cur)-added]) {
			return cur[:added], true
		}
	}
	return nil, len(cur) == 0
}

// endedSelfTest is the job's outcome from the drive's log entry of its test.
// A test a reset interrupted, as a reboot does, leaves the job interrupted.
func endedSelfTest(entry selfTestEntry, device, testType string) (SmartTestResult, error) {
	switch entry.Result {
	case "completed":
		return SmartTestResult{Success: true, Device: device, Test: testType, Status: "completed", Message: entry.Text}, nil
	case "interrupted":
		return SmartTestResult{}, fmt.Errorf("%w: /dev/%s reports the self-test was interrupted by a reset", bridgeipc.ErrNotResumed, device)
	default:
		return SmartTestResult{}, fmt.Errorf("SMART %s self-test on /dev/%s %s: %s", testType, device, entry.Result, entry.Text)
	}
}

type smartTestTaskState struct {
	task            *bridgeipc.Task
	device          string
	testType        string
	interval        time.Duration
	timeout         time.Duration
	polls           int
	seenInProgress  bool
	lastPercent     int
	progressedAt    time.Time
	consecutiveErrs int
}

func (s *smartTestTaskState) reportStart() {
	s.task.ReportProgress(SmartTestProgress{
		Type:     "status",
		Device:   s.device,
		TestType: s.testType,
		Status:   "starting",
		Message:  fmt.Sprintf("Starting SMART %s self-test", s.testType),
	})
}

func (s *smartTestTaskState) pollInitial(ctx context.Context) {
	// Immediate first poll, but only emit if it observes in_progress.
	// Anything else here is almost certainly stale residue.
	st, err := pollSmartStatus(ctx, s.device)
	if err != nil {
		return
	}
	s.polls++
	if st.State == "in_progress" {
		s.seenInProgress = true
		s.emit(st)
	}
}

func (s *smartTestTaskState) pollUntilDone(ctx context.Context) (SmartTestResult, error) {
	s.lastPercent = -1
	s.progressedAt = time.Now()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Returning context.Canceled routes to the Task's canceled state,
			// not markFailed.
			return SmartTestResult{}, ctx.Err()
		case <-ticker.C:
			result, done, err := s.poll(ctx)
			if err != nil {
				return SmartTestResult{}, err
			}
			if done {
				return result, nil
			}
			if time.Since(s.progressedAt) > s.timeout {
				return SmartTestResult{}, errSmartTestTimeout
			}
		}
	}
}

func (s *smartTestTaskState) poll(ctx context.Context) (SmartTestResult, bool, error) {
	st, err := pollSmartStatus(ctx, s.device)
	if err != nil {
		return SmartTestResult{}, false, s.handlePollError(ctx, err)
	}
	s.consecutiveErrs = 0
	s.polls++

	if st.State == "in_progress" {
		s.seenInProgress = true
		if st.PercentComplete != s.lastPercent {
			s.lastPercent = st.PercentComplete
			s.progressedAt = time.Now()
		}
		s.emit(st)
		return SmartTestResult{}, false, nil
	}
	// Early terminal states may be stale residue; trust them after a few polls.
	if !s.seenInProgress && s.polls < smartTerminalPolls {
		s.emit(st)
		return SmartTestResult{}, false, nil
	}

	s.emit(st)
	if s.completed(st) {
		return s.result(st), true, nil
	}
	if st.State == "idle" {
		return SmartTestResult{}, false, errors.New("SMART self-test never started")
	}
	return SmartTestResult{}, false, errors.New(st.Message)
}

func (s *smartTestTaskState) handlePollError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return ctx.Err()
	}
	s.consecutiveErrs++
	if s.consecutiveErrs >= 3 {
		return err
	}
	return nil
}

func (s *smartTestTaskState) emit(st SmartTestStatus) {
	pct := st.PercentComplete
	s.task.ReportProgress(SmartTestProgress{
		Type:       "status",
		Device:     s.device,
		TestType:   s.testType,
		Status:     s.progressStatus(st.State),
		Message:    st.Message,
		Percentage: &pct,
	})
}

func (s *smartTestTaskState) progressStatus(status string) string {
	if status != "idle" {
		return status
	}
	if s.seenInProgress {
		return "completed"
	}
	return "starting"
}

func (s *smartTestTaskState) completed(st SmartTestStatus) bool {
	return st.State == "completed" || (st.State == "idle" && s.seenInProgress)
}

func (s *smartTestTaskState) result(st SmartTestStatus) SmartTestResult {
	return SmartTestResult{Success: true, Device: s.device, Test: s.testType, Status: "completed", Message: st.Message, Duration: nil}
}
