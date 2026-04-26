package storage

import (
	"context"
	"fmt"

	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
)

const JobTypeStorageSmartTest = "storage.smart_test"

type SmartTestProgress struct {
	Type     string `json:"type"`
	Device   string `json:"device,omitempty"`
	TestType string `json:"test_type,omitempty"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
}

func RegisterJobRunners() {
	bridgejobs.RegisterRunner(JobTypeStorageSmartTest, runSmartTestJob)
}

func runSmartTestJob(_ context.Context, job *bridgejobs.Job, args []string) (any, error) {
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

	result, err := RunSmartTest(device, testType)
	if err != nil {
		return nil, bridgejobs.NewError(err.Error(), 500)
	}

	job.ReportProgress(SmartTestProgress{
		Type:     "status",
		Device:   device,
		TestType: testType,
		Status:   "completed",
		Message:  fmt.Sprintf("SMART %s self-test started on /dev/%s", testType, device),
	})

	return result, nil
}
