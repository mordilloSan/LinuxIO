package storage

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const JobTypeStorageSmartTest = "storage.run_smart_test"

type SmartTestProgress struct {
	Type     string `json:"type"`
	Device   string `json:"device,omitempty"`
	TestType string `json:"test_type,omitempty"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
}

func RegisterJobRoutes(router *bridgejobs.Router) {
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route:  JobTypeStorageSmartTest,
		Runner: runSmartTestJob,
		Policy: bridgejobs.ActionDefault,
	})
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

	result, err := RunSmartTest(ctx, device, testType)
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
