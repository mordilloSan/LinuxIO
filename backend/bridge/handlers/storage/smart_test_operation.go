package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	storageapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage/api"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type SmartTestProgress struct {
	Type       string `json:"type"`
	Device     string `json:"device,omitempty"`
	TestType   string `json:"test_type,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	Percentage *int   `json:"percentage,omitempty"`
}

func RegisterJobRoutes(router *bridgejobs.Router) {
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route:  storageapi.RunSmartTest,
		Runner: runSmartTestJob,
		Policy: bridgejobs.ActionDefault,
	})
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

func runSmartTestJob(ctx context.Context, job *bridgejobs.Job, req apischema.DeviceTestTypeRequest) (any, error) {
	state := smartTestJobState{
		job:      job,
		device:   req.Device,
		testType: req.TestType,
	}
	state.reportStart()

	if _, err := RunSmartTest(ctx, req.Device, req.TestType); err != nil {
		return nil, bridgejobs.NewError(err.Error(), 500)
	}

	state.pollInitial(ctx)
	return state.pollUntilDone(ctx)
}

type smartTestJobState struct {
	job             *bridgejobs.Job
	device          string
	testType        string
	seenInProgress  bool
	consecutiveErrs int
}

func (s *smartTestJobState) reportStart() {
	s.job.ReportProgress(SmartTestProgress{
		Type:     "status",
		Device:   s.device,
		TestType: s.testType,
		Status:   "starting",
		Message:  fmt.Sprintf("Starting SMART %s self-test", s.testType),
	})
}

func (s *smartTestJobState) pollInitial(ctx context.Context) {
	// Immediate first poll, but only emit if it observes in_progress.
	// Anything else here is almost certainly stale residue.
	if st, err := PollSmartTestStatus(ctx, s.device); err == nil && st.State == "in_progress" {
		s.seenInProgress = true
		s.emit(st)
	}
}

func (s *smartTestJobState) pollUntilDone(ctx context.Context) (any, error) {
	ticker := time.NewTicker(pollInterval(s.testType))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Returning context.Canceled routes to markCanceled (jobs.go),
			// not markFailed.
			return nil, ctx.Err()
		case <-ticker.C:
			result, done, err := s.poll(ctx)
			if err != nil {
				return nil, err
			}
			if done {
				return result, nil
			}
		}
	}
}

func (s *smartTestJobState) poll(ctx context.Context) (any, bool, error) {
	st, err := PollSmartTestStatus(ctx, s.device)
	if err != nil {
		return nil, false, s.handlePollError(ctx, err)
	}
	s.consecutiveErrs = 0

	// Don't accept terminal status until we've actually seen the test run.
	if !s.seenInProgress && st.State != "in_progress" {
		s.emit(st)
		return nil, false, nil
	}
	if st.State == "in_progress" {
		s.seenInProgress = true
		s.emit(st)
		return nil, false, nil
	}

	s.emit(st)
	if s.completed(st) {
		return s.result(st), true, nil
	}
	return nil, false, bridgejobs.NewError(st.Message, 500)
}

func (s *smartTestJobState) handlePollError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return ctx.Err()
	}
	s.consecutiveErrs++
	if s.consecutiveErrs >= 3 {
		return bridgejobs.NewError(err.Error(), 500)
	}
	return nil
}

func (s *smartTestJobState) emit(st SmartTestStatus) {
	pct := st.PercentComplete
	s.job.ReportProgress(SmartTestProgress{
		Type:       "status",
		Device:     s.device,
		TestType:   s.testType,
		Status:     s.progressStatus(st.State),
		Message:    st.Message,
		Percentage: &pct,
	})
}

func (s *smartTestJobState) progressStatus(status string) string {
	if status != "idle" {
		return status
	}
	if s.seenInProgress {
		return "completed"
	}
	return "starting"
}

func (s *smartTestJobState) completed(st SmartTestStatus) bool {
	return st.State == "completed" || (st.State == "idle" && s.seenInProgress)
}

func (s *smartTestJobState) result(st SmartTestStatus) map[string]any {
	return map[string]any{
		"success":  true,
		"device":   s.device,
		"test":     s.testType,
		"status":   "completed",
		"message":  st.Message,
		"duration": nil,
	}
}
