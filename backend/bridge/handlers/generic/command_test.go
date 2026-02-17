package generic

import (
	"testing"
)

// Test that direct handler is disabled
func TestDisabledExecHandler(t *testing.T) {
	handlers := CommandHandlers()
	execHandler := handlers["exec"]

	_, err := execHandler([]string{"echo 'test'"})
	if err == nil {
		t.Fatal("expected error when calling disabled handler")
	}

	expectedMsg := "direct command execution is disabled - commands must be defined in module YAML files"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
	}
}

// Test the internal ExecCommandDirect function (used by modules)
func TestExecCommandDirect_SimpleEcho(t *testing.T) {
	result, err := ExecCommandDirect("echo 'Hello World'", "10")
	if err != nil {
		t.Fatalf("ExecCommandDirect failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	exitCode, ok := resultMap["exitCode"].(int)
	if !ok {
		t.Fatalf("exitCode is not an int: %T", resultMap["exitCode"])
	}
	if exitCode != 0 {
		t.Errorf("expected exitCode 0, got %v", exitCode)
	}

	stdout, ok := resultMap["stdout"].(string)
	if !ok {
		t.Fatalf("stdout is not a string: %T", resultMap["stdout"])
	}
	if stdout != "Hello World\n" {
		t.Errorf("expected 'Hello World\\n', got %q", stdout)
	}
}

func TestExecCommandDirect_JSONOutput(t *testing.T) {
	result, err := ExecCommandDirect(`echo '{"status":"ok","value":42}'`, "10")
	if err != nil {
		t.Fatalf("ExecCommandDirect failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	status, ok := resultMap["status"].(string)
	if !ok {
		t.Fatalf("status is not a string: %T", resultMap["status"])
	}
	if status != "ok" {
		t.Errorf("expected status 'ok', got %v", status)
	}

	value, ok := resultMap["value"].(float64)
	if !ok {
		t.Fatalf("value is not a float64: %T", resultMap["value"])
	}
	if value != 42 {
		t.Errorf("expected value 42, got %v", value)
	}
}

func TestExecCommandDirect_FailedCommand(t *testing.T) {
	result, err := ExecCommandDirect("false", "10")
	if err != nil {
		t.Fatalf("ExecCommandDirect should not return error for failed command: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	exitCode, ok := resultMap["exitCode"].(int)
	if !ok {
		t.Fatalf("exitCode is not an int: %T", resultMap["exitCode"])
	}
	if exitCode == 0 {
		t.Errorf("expected non-zero exitCode for 'false' command")
	}

	if resultMap["error"] == nil {
		t.Errorf("expected error field to be set")
	}
}

func TestExecCommandDirect_WithTimeout(t *testing.T) {
	// Should complete within 2 seconds
	result, err := ExecCommandDirect("sleep 0.1", "2")
	if err != nil {
		t.Fatalf("ExecCommandDirect failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	exitCode, ok := resultMap["exitCode"].(int)
	if !ok {
		t.Fatalf("exitCode is not an int: %T", resultMap["exitCode"])
	}
	if exitCode != 0 {
		t.Errorf("expected exitCode 0, got %v", exitCode)
	}
}
