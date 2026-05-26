package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const JobTypeStorageSmartTest = "storage.run_smart_test"

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
		Route:  JobTypeStorageSmartTest,
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

func runSmartTestJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	if len(args) < 2 {
		return nil, bridgejobs.NewError("run_smart_test requires device name and test type (short/long)", 400)
	}
	device := args[0]
	testType := args[1]

	job.ReportProgress(SmartTestProgress{
		Type:     "status",
		Device:   device,
		TestType: testType,
		Status:   "starting",
		Message:  fmt.Sprintf("Starting SMART %s self-test", testType),
	})

	if _, err := RunSmartTest(ctx, device, testType); err != nil {
		return nil, bridgejobs.NewError(err.Error(), 500)
	}

	// seenInProgress gates two specific hazards:
	//   - An immediate post-`-t` poll can return the *previous* test's
	//     terminal status before the drive has picked up the new one.
	//   - When the drive eventually clears its self-test log, an "idle"
	//     state after a real completion should be treated as completed,
	//     not as a fresh start.
	seenInProgress := false

	emit := func(st SmartTestStatus) {
		status := st.State
		if status == "idle" {
			if seenInProgress {
				status = "completed"
			} else {
				status = "starting"
			}
		}
		pct := st.PercentComplete
		job.ReportProgress(SmartTestProgress{
			Type:       "status",
			Device:     device,
			TestType:   testType,
			Status:     status,
			Message:    st.Message,
			Percentage: &pct,
		})
	}

	resultMap := func(st SmartTestStatus) map[string]any {
		return map[string]any{
			"success":  true,
			"device":   device,
			"test":     testType,
			"status":   "completed",
			"message":  st.Message,
			"duration": nil,
		}
	}

	// Immediate first poll, but only emit if it observes in_progress.
	// Anything else here is almost certainly stale residue.
	if st, err := PollSmartTestStatus(ctx, device); err == nil && st.State == "in_progress" {
		seenInProgress = true
		emit(st)
	}

	ticker := time.NewTicker(pollInterval(testType))
	defer ticker.Stop()
	consecutiveErrs := 0
	for {
		select {
		case <-ctx.Done():
			// Returning context.Canceled routes to markCanceled (jobs.go),
			// not markFailed.
			return nil, ctx.Err()
		case <-ticker.C:
			st, err := PollSmartTestStatus(ctx, device)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return nil, ctx.Err()
				}
				consecutiveErrs++
				if consecutiveErrs >= 3 {
					return nil, bridgejobs.NewError(err.Error(), 500)
				}
				continue
			}
			consecutiveErrs = 0

			// Don't accept terminal status until we've actually seen the test run.
			if !seenInProgress && st.State != "in_progress" {
				emit(st)
				continue
			}
			if st.State == "in_progress" {
				seenInProgress = true
				emit(st)
				continue
			}

			emit(st)
			if st.State == "completed" || (st.State == "idle" && seenInProgress) {
				return resultMap(st), nil
			}
			return nil, bridgejobs.NewError(st.Message, 500)
		}
	}
}
