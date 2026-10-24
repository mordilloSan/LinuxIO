package storage

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestParseSmartTestJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantState   string
		wantPercent int
	}{
		{
			name: "ATA in-progress with remaining_percent",
			input: `{
				"ata_smart_data": {
					"self_test": {
						"status": {
							"value": 249,
							"string": "Self-test routine in progress",
							"remaining_percent": 60
						}
					}
				}
			}`,
			wantState:   "in_progress",
			wantPercent: 40,
		},
		{
			name: "ATA completed (passed true, no remaining_percent)",
			input: `{
				"ata_smart_data": {
					"self_test": {
						"status": {
							"value": 0,
							"string": "Completed without error",
							"passed": true
						}
					}
				}
			}`,
			wantState:   "completed",
			wantPercent: 100,
		},
		{
			name: "ATA failed (passed false)",
			input: `{
				"ata_smart_data": {
					"self_test": {
						"status": {
							"value": 117,
							"string": "Completed: read failure",
							"passed": false
						}
					}
				}
			}`,
			wantState:   "failed",
			wantPercent: 0,
		},
		{
			name: "NVMe in-progress, real smartmontools shape (object op + scalar pct)",
			input: `{
				"nvme_self_test_log": {
					"current_self_test_operation": { "value": 1, "string": "Short self-test" },
					"current_self_test_completion_percent": 30
				}
			}`,
			wantState:   "in_progress",
			wantPercent: 30,
		},
		{
			name: "NVMe in-progress, fully-scalar fallback",
			input: `{
				"nvme_self_test_log": {
					"current_self_test_op": 1,
					"current_self_test_completion_percent": 30
				}
			}`,
			wantState:   "in_progress",
			wantPercent: 30,
		},
		{
			name: "NVMe in-progress, fully-object fallback",
			input: `{
				"nvme_self_test_log": {
					"current_self_test_operation": { "value": 1 },
					"current_self_test_completion": { "value": 30 }
				}
			}`,
			wantState:   "in_progress",
			wantPercent: 30,
		},
		{
			name: "NVMe completed (op=0, table[0].result=0)",
			input: `{
				"nvme_self_test_log": {
					"current_self_test_operation": { "value": 0 },
					"table": [
						{ "self_test_result": { "value": 0, "string": "Completed without error" } }
					]
				}
			}`,
			wantState:   "completed",
			wantPercent: 100,
		},
		{
			name: "NVMe aborted by host (result=1)",
			input: `{
				"nvme_self_test_log": {
					"table": [ { "self_test_result": { "value": 1 } } ]
				}
			}`,
			wantState: "aborted",
		},
		{
			name: "NVMe aborted by format (result=4) — guards against 1-3/4-8 misclassification",
			input: `{
				"nvme_self_test_log": {
					"table": [ { "self_test_result": { "value": 4 } } ]
				}
			}`,
			wantState: "aborted",
		},
		{
			name: "NVMe aborted unknown (result=8) — guards against 1-3/4-8 misclassification",
			input: `{
				"nvme_self_test_log": {
					"table": [ { "self_test_result": { "value": 8 } } ]
				}
			}`,
			wantState: "aborted",
		},
		{
			name: "NVMe failed fatal (result=5)",
			input: `{
				"nvme_self_test_log": {
					"table": [ { "self_test_result": { "value": 5 } } ]
				}
			}`,
			wantState: "failed",
		},
		{
			name: "NVMe failed segment (result=7)",
			input: `{
				"nvme_self_test_log": {
					"table": [ { "self_test_result": { "value": 7 } } ]
				}
			}`,
			wantState: "failed",
		},
		{
			name:      "Empty / both blocks missing",
			input:     `{}`,
			wantState: "idle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSmartTestJSON([]byte(tt.input))
			if err != nil {
				t.Fatalf("parseSmartTestJSON: %v", err)
			}
			if got.State != tt.wantState {
				t.Errorf("state: got %q, want %q", got.State, tt.wantState)
			}
			if tt.wantState == "in_progress" || tt.wantState == "completed" {
				if got.PercentComplete != tt.wantPercent {
					t.Errorf("percent: got %d, want %d", got.PercentComplete, tt.wantPercent)
				}
			}
		})
	}
}

func TestInterpretSmartctlResult(t *testing.T) {
	validInProgress := []byte(`{
		"ata_smart_data": {
			"self_test": {
				"status": { "value": 249, "remaining_percent": 60 }
			}
		}
	}`)
	garbage := []byte(`not json at all`)

	// Synthesize a real *exec.ExitError. exec.Command on a no-op-but-failing
	// shell produces one without needing a fixture binary.
	failingCmd := exec.Command("sh", "-c", "exit 4")
	exitErr := failingCmd.Run()
	if _, ok := exitErr.(*exec.ExitError); !ok {
		t.Fatalf("setup: expected *exec.ExitError, got %T", exitErr)
	}

	tests := []struct {
		name      string
		out       []byte
		runErr    error
		wantState string
		wantErr   bool
	}{
		{
			name:      "valid JSON + nil err",
			out:       validInProgress,
			runErr:    nil,
			wantState: "in_progress",
		},
		{
			name:      "valid JSON + non-zero exit (typical for unhealthy drives)",
			out:       validInProgress,
			runErr:    exitErr,
			wantState: "in_progress",
		},
		{
			name:    "garbage + exit error → parse error surfaced",
			out:     garbage,
			runErr:  exitErr,
			wantErr: true,
		},
		{
			name:    "garbage + nil error → parse error",
			out:     garbage,
			runErr:  nil,
			wantErr: true,
		},
		{
			name:    "empty output → parse error",
			out:     nil,
			runErr:  errors.New("some failure"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := interpretSmartctlResult(tt.out, tt.runErr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got status=%q", got.State)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.State != tt.wantState {
				t.Errorf("state: got %q, want %q", got.State, tt.wantState)
			}
		})
	}
}

func newSmartTestState(t *testing.T, poll func(context.Context, string) (SmartTestStatus, error), timeout time.Duration) *smartTestTaskState {
	t.Helper()
	orig := pollSmartStatus
	t.Cleanup(func() { pollSmartStatus = orig })
	pollSmartStatus = poll

	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity("storage.run_smart_test", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return &smartTestTaskState{task: task, device: "sda", testType: "short", interval: time.Millisecond, timeout: timeout}
}

func TestSmartTestAcceptsTerminalStateAfterNPolls(t *testing.T) {
	polls := 0
	state := newSmartTestState(t, func(context.Context, string) (SmartTestStatus, error) {
		polls++
		return SmartTestStatus{State: "completed", PercentComplete: 100}, nil
	}, time.Minute)

	state.pollInitial(t.Context())
	result, err := state.pollUntilDone(t.Context())
	if err != nil {
		t.Fatalf("pollUntilDone: %v", err)
	}
	if !result.Success || polls != smartTerminalPolls {
		t.Fatalf("success=%v polls=%d, want success after %d polls", result.Success, polls, smartTerminalPolls)
	}
}

func TestSmartTestFailsWhenNeverStarted(t *testing.T) {
	state := newSmartTestState(t, func(context.Context, string) (SmartTestStatus, error) {
		return SmartTestStatus{State: "idle"}, nil
	}, time.Minute)

	state.pollInitial(t.Context())
	if _, err := state.pollUntilDone(t.Context()); err == nil {
		t.Fatal("expected idle self-test to fail after the poll threshold")
	}
}

func TestSmartTestStallFailsTask(t *testing.T) {
	state := newSmartTestState(t, func(context.Context, string) (SmartTestStatus, error) {
		return SmartTestStatus{State: "in_progress"}, nil
	}, 20*time.Millisecond)

	_, err := state.pollUntilDone(t.Context())
	if !errors.Is(err, errSmartTestTimeout) {
		t.Fatalf("err = %v, want timeout task error", err)
	}
}

func TestSmartTestMovingProgressOutlivesStallTimeout(t *testing.T) {
	polls := 0
	state := newSmartTestState(t, func(context.Context, string) (SmartTestStatus, error) {
		polls++
		if polls > 60 {
			return SmartTestStatus{State: "completed", PercentComplete: 100}, nil
		}
		return SmartTestStatus{State: "in_progress", PercentComplete: polls}, nil
	}, 20*time.Millisecond)

	result, err := state.pollUntilDone(t.Context())
	if err != nil || !result.Success {
		t.Fatalf("result=%+v err=%v, want success while progress keeps moving past the stall timeout", result, err)
	}
}
