package transientunit

import (
	"fmt"
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

func TestIsNoSuchUnitErrorRecognizesWrappedDBusError(t *testing.T) {
	err := fmt.Errorf("stop unit: %w", godbus.Error{
		Name: "org.freedesktop.systemd1.NoSuchUnit",
		Body: []any{"unit not found"},
	})
	if !isNoSuchUnitError(err) {
		t.Fatalf("isNoSuchUnitError(%v) = false, want true", err)
	}
}
