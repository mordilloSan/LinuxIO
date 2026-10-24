package handlers

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/logguard"
)

// TestHandlersReturnOrLogErrorsNotBoth enforces "log or return, never both".
// The bridge router and task runner log every failed route and task exactly
// once, so a handler that logs an error and then returns it writes the same
// failure to the journal twice.
func TestHandlersReturnOrLogErrorsNotBoth(t *testing.T) {
	if err := walkGoFiles(func(path string) error {
		violations, err := logguard.LogThenReturn(path)
		for _, line := range violations {
			t.Errorf("%s", line)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
