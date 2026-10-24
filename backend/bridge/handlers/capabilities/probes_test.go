package capabilities

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func withMonitoringHealth(t *testing.T, err error) {
	t.Helper()
	orig := checkMonitoringHealth
	checkMonitoringHealth = func(context.Context) error { return err }
	t.Cleanup(func() { checkMonitoringHealth = orig })
}

func TestCheckMonitoringAvailabilityReportsHealthyDaemon(t *testing.T) {
	withMonitoringHealth(t, nil)

	ok, err := checkMonitoringAvailability(context.Background())
	if err != nil {
		t.Fatalf("checkMonitoringAvailability: %v", err)
	}
	if !ok {
		t.Fatal("checkMonitoringAvailability returned false")
	}
}

func TestCheckMonitoringAvailabilityReportsUnhealthyBody(t *testing.T) {
	withMonitoringHealth(t, errors.New("linuxio-monitoring health 503 Service Unavailable: latest tick is stale"))

	ok, err := checkMonitoringAvailability(context.Background())
	if err == nil || !strings.Contains(err.Error(), "latest tick is stale") {
		t.Fatalf("error = %v, want health body", err)
	}
	if ok {
		t.Fatal("checkMonitoringAvailability returned true")
	}
}

func TestCheckMemoryModuleInventoryAvailabilityUsesUdev(t *testing.T) {
	lookPathCalled := false
	restore := stubMemoryInventoryCommands(t,
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "udevadm" {
				t.Fatalf("unexpected command %q", name)
			}
			wantArgs := []string{"info", "--query=property", "--path=/sys/class/dmi/id"}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("udevadm args = %v, want %v", args, wantArgs)
			}
			return []byte(`MEMORY_ARRAY_NUM_DEVICES=1
MEMORY_DEVICE_0_SIZE=8589934592
`), nil
		},
		func(string) (string, error) {
			lookPathCalled = true
			return "", errors.New("should not be called")
		},
	)
	defer restore()

	available, err := checkMemoryModuleInventoryAvailability(context.Background())
	if err != nil {
		t.Fatalf("checkMemoryModuleInventoryAvailability() error = %v", err)
	}
	if !available {
		t.Fatal("checkMemoryModuleInventoryAvailability() available = false, want true")
	}
	if lookPathCalled {
		t.Fatal("dmidecode lookup was called despite udev inventory")
	}
}

func TestCheckMemoryModuleInventoryAvailabilityUsesDMIDecodeFallback(t *testing.T) {
	restore := stubMemoryInventoryCommands(t,
		func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name != "udevadm" {
				t.Fatalf("unexpected command %q", name)
			}
			return []byte("P: /devices/virtual/dmi/id\n"), nil
		},
		func(name string) (string, error) {
			if name != "dmidecode" {
				t.Fatalf("unexpected lookup %q", name)
			}
			return "/usr/sbin/dmidecode", nil
		},
	)
	defer restore()

	available, err := checkMemoryModuleInventoryAvailability(context.Background())
	if err != nil {
		t.Fatalf("checkMemoryModuleInventoryAvailability() error = %v", err)
	}
	if !available {
		t.Fatal("checkMemoryModuleInventoryAvailability() available = false, want true")
	}
}

func TestCheckMemoryModuleInventoryAvailabilityReportsMissingSources(t *testing.T) {
	restore := stubMemoryInventoryCommands(t,
		func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name != "udevadm" {
				t.Fatalf("unexpected command %q", name)
			}
			return nil, errors.New("udevadm not found")
		},
		func(name string) (string, error) {
			if name != "dmidecode" {
				t.Fatalf("unexpected lookup %q", name)
			}
			return "", errors.New("dmidecode not found")
		},
	)
	defer restore()

	available, err := checkMemoryModuleInventoryAvailability(context.Background())
	if available {
		t.Fatal("checkMemoryModuleInventoryAvailability() available = true, want false")
	}
	if err == nil || !strings.Contains(err.Error(), "dmidecode is not installed") {
		t.Fatalf("checkMemoryModuleInventoryAvailability() error = %v", err)
	}
}

func stubMemoryInventoryCommands(
	t *testing.T,
	run func(context.Context, string, ...string) ([]byte, error),
	lookPath func(string) (string, error),
) func() {
	t.Helper()
	originalRunCommand := memoryInventoryRunCommand
	originalLookPath := memoryInventoryLookPath
	memoryInventoryRunCommand = run
	memoryInventoryLookPath = lookPath
	return func() {
		memoryInventoryRunCommand = originalRunCommand
		memoryInventoryLookPath = originalLookPath
	}
}
