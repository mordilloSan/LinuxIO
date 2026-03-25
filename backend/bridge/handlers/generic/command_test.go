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

	expectedMsg := "direct command execution is disabled"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
	}
}
