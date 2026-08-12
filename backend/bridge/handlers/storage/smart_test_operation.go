package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type SmartTestProgress struct {
	Type       string `json:"type"`
	Device     string `json:"device,omitempty"`
	TestType   string `json:"test_type,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	Percentage *int   `json:"percentage,omitempty"`
}

func (p SmartTestProgress) ProgressEnvelope() bridgetasks.TaskProgress {
	return bridgetasks.TaskProgress{
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

var smartTestRoutes = smartTestBindings().Routes()

func smartTestBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.DeviceTestTypeRequest, SmartTestResult]("storage.run_smart_test", apischema.SessionTask(), apischema.WithTaskProgress[SmartTestProgress](), apischema.WithTaskMetadata(func(req apischema.DeviceTestTypeRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.Device, req.TestType}, Label: "Running SMART self-test", Device: req.Device, TestType: req.TestType}
		})).Run(runSmartTestTask, bridgetasks.TaskDefault),
	)
}

func RegisterTaskRoutes(router *bridgetasks.Router) {
	smartTestBindings().Register(router)
}

// pollInterval picks how often to poll smartctl based on test type. Short tests
// finish in ~2 minutes so a tighter loop gives smoother progress; long tests
// can run for hours and don't benefit from frequent polling.
func pollInterval(testType string) time.Duration {
	if testType == "long" {
		return 60 * time.Second
	}
	return 15 * time.Second
}

func runSmartTestTask(ctx context.Context, task *bridgetasks.Task, req apischema.DeviceTestTypeRequest) (SmartTestResult, error) {
	state := smartTestTaskState{
		task:     task,
		device:   req.Device,
		testType: req.TestType,
	}
	state.reportStart()

	if _, err := RunSmartTest(ctx, req.Device, req.TestType); err != nil {
		return SmartTestResult{}, bridgetasks.NewError(err.Error(), 500)
	}

	state.pollInitial(ctx)
	return state.pollUntilDone(ctx)
}

type smartTestTaskState struct {
	task            *bridgetasks.Task
	device          string
	testType        string
	seenInProgress  bool
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
	if st, err := PollSmartTestStatus(ctx, s.device); err == nil && st.State == "in_progress" {
		s.seenInProgress = true
		s.emit(st)
	}
}

func (s *smartTestTaskState) pollUntilDone(ctx context.Context) (SmartTestResult, error) {
	ticker := time.NewTicker(pollInterval(s.testType))
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
		}
	}
}

func (s *smartTestTaskState) poll(ctx context.Context) (SmartTestResult, bool, error) {
	st, err := PollSmartTestStatus(ctx, s.device)
	if err != nil {
		return SmartTestResult{}, false, s.handlePollError(ctx, err)
	}
	s.consecutiveErrs = 0

	// Don't accept terminal status until we've actually seen the test run.
	if !s.seenInProgress && st.State != "in_progress" {
		s.emit(st)
		return SmartTestResult{}, false, nil
	}
	if st.State == "in_progress" {
		s.seenInProgress = true
		s.emit(st)
		return SmartTestResult{}, false, nil
	}

	s.emit(st)
	if s.completed(st) {
		return s.result(st), true, nil
	}
	return SmartTestResult{}, false, bridgetasks.NewError(st.Message, 500)
}

func (s *smartTestTaskState) handlePollError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return ctx.Err()
	}
	s.consecutiveErrs++
	if s.consecutiveErrs >= 3 {
		return bridgetasks.NewError(err.Error(), 500)
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
