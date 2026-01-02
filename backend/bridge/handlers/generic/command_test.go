package generic

import (
	"testing"
)

func TestExecCommand_SimpleEcho(t *testing.T) {
	result, err := execCommand([]string{"echo 'Hello World'"})
	if err != nil {
		t.Fatalf("execCommand failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if resultMap["exitCode"].(int) != 0 {
		t.Errorf("expected exitCode 0, got %v", resultMap["exitCode"])
	}

	stdout := resultMap["stdout"].(string)
	if stdout != "Hello World\n" {
		t.Errorf("expected 'Hello World\\n', got %q", stdout)
	}
}

func TestExecCommand_JSONOutput(t *testing.T) {
	result, err := execCommand([]string{`echo '{"status":"ok","value":42}'`})
	if err != nil {
		t.Fatalf("execCommand failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if resultMap["status"].(string) != "ok" {
		t.Errorf("expected status 'ok', got %v", resultMap["status"])
	}

	if resultMap["value"].(float64) != 42 {
		t.Errorf("expected value 42, got %v", resultMap["value"])
	}
}

func TestExecCommand_FailedCommand(t *testing.T) {
	result, err := execCommand([]string{"false"})
	if err != nil {
		t.Fatalf("execCommand should not return error for failed command: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if resultMap["exitCode"].(int) == 0 {
		t.Errorf("expected non-zero exitCode for 'false' command")
	}

	if resultMap["error"] == nil {
		t.Errorf("expected error field to be set")
	}
}

func TestExecCommand_WithTimeout(t *testing.T) {
	// Should complete within 2 seconds
	result, err := execCommand([]string{"sleep 0.1", "2"})
	if err != nil {
		t.Fatalf("execCommand failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if resultMap["exitCode"].(int) != 0 {
		t.Errorf("expected exitCode 0, got %v", resultMap["exitCode"])
	}
}

func TestExecCommand_NoCommand(t *testing.T) {
	_, err := execCommand([]string{})
	if err == nil {
		t.Fatal("expected error when no command provided")
	}

	if err.Error() != "no command provided" {
		t.Errorf("expected 'no command provided' error, got %v", err)
	}
}
