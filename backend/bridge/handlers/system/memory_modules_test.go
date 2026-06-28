package system

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestParseUdevMemoryModules(t *testing.T) {
	modules := parseUdevMemoryModules(`P: /devices/virtual/mem/null
E: SUBSYSTEM=mem

P: /devices/virtual/dmi/id
E: MEMORY_ARRAY_NUM_DEVICES=2
E: MEMORY_DEVICE_0_BANK_LOCATOR=BANK 0
E: MEMORY_DEVICE_0_LOCATOR=DIMM_A1
E: MEMORY_DEVICE_0_MEMORY_TECHNOLOGY=DRAM
E: MEMORY_DEVICE_0_RANK=2
E: MEMORY_DEVICE_0_SIZE=17179869184
E: MEMORY_DEVICE_0_SPEED_MTS=3200
E: MEMORY_DEVICE_0_TOTAL_WIDTH=64
E: MEMORY_DEVICE_0_TYPE=DDR4
E: MEMORY_DEVICE_1_BANK_LOCATOR=BANK 1
E: MEMORY_DEVICE_1_LOCATOR=DIMM_B1
E: MEMORY_DEVICE_1_PRESENT=0
`)

	want := []apischema.MemoryModule{
		{
			ID:         "BANK 0: DIMM_A1",
			Technology: "DRAM",
			Type:       "DDR4",
			Size:       "16 GiB",
			State:      "Present",
			Rank:       "2",
			Speed:      "3200 MT/s",
		},
		{
			ID:         "BANK 1: DIMM_B1",
			Technology: "Unknown",
			Type:       "Unknown",
			Size:       "Unknown",
			State:      "Absent",
			Rank:       "Unknown",
			Speed:      "Unknown",
		},
	}
	if !reflect.DeepEqual(modules, want) {
		t.Fatalf("parseUdevMemoryModules() = %+v, want %+v", modules, want)
	}
}

func TestFetchMemoryModulesFallsBackToDMIDecode(t *testing.T) {
	restore := stubMemoryModuleCommands(t,
		func(_ context.Context, name string, _ ...string) ([]byte, error) {
			switch name {
			case "udevadm":
				return []byte("P: /devices/virtual/dmi/id\n"), nil
			case "dmidecode":
				return []byte(`Memory Device
	Size: 8192 MB
	Locator: DIMM_A1
	Bank Locator: BANK 0
	Type: DDR4
	Rank: 1
	Speed: 2666 MT/s
	Memory Technology: DRAM
`), nil
			default:
				t.Fatalf("unexpected command %q", name)
				return nil, nil
			}
		},
		nil,
	)
	defer restore()

	modules, err := FetchMemoryModules(context.Background())
	if err != nil {
		t.Fatalf("FetchMemoryModules() error = %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("FetchMemoryModules() returned %d modules, want 1", len(modules))
	}
	if modules[0].ID != "BANK 0: DIMM_A1" || modules[0].Size != "8192 MB" {
		t.Fatalf("FetchMemoryModules() module = %+v", modules[0])
	}
}

func TestFetchMemoryModulesReturnsEmptyWhenInventoryUnavailable(t *testing.T) {
	restore := stubMemoryModuleCommands(t,
		func(_ context.Context, name string, _ ...string) ([]byte, error) {
			switch name {
			case "udevadm":
				return nil, errors.New("udevadm not found")
			case "dmidecode":
				return nil, errors.New("dmidecode not found")
			default:
				t.Fatalf("unexpected command %q", name)
				return nil, nil
			}
		},
		nil,
	)
	defer restore()

	modules, err := FetchMemoryModules(context.Background())
	if err != nil {
		t.Fatalf("FetchMemoryModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Fatalf("FetchMemoryModules() returned %+v, want empty", modules)
	}
}

func TestCheckMemoryModuleInventoryAvailabilityUsesUdev(t *testing.T) {
	lookPathCalled := false
	restore := stubMemoryModuleCommands(t,
		func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name != "udevadm" {
				t.Fatalf("unexpected command %q", name)
			}
			return []byte(`P: /devices/virtual/dmi/id
E: MEMORY_ARRAY_NUM_DEVICES=1
E: MEMORY_DEVICE_0_SIZE=8589934592
`), nil
		},
		func(string) (string, error) {
			lookPathCalled = true
			return "", errors.New("should not be called")
		},
	)
	defer restore()

	available, err := CheckMemoryModuleInventoryAvailability(context.Background())
	if err != nil {
		t.Fatalf("CheckMemoryModuleInventoryAvailability() error = %v", err)
	}
	if !available {
		t.Fatal("CheckMemoryModuleInventoryAvailability() available = false, want true")
	}
	if lookPathCalled {
		t.Fatal("dmidecode lookup was called despite udev inventory")
	}
}

func TestCheckMemoryModuleInventoryAvailabilityUsesDMIDecodeFallback(t *testing.T) {
	restore := stubMemoryModuleCommands(t,
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

	available, err := CheckMemoryModuleInventoryAvailability(context.Background())
	if err != nil {
		t.Fatalf("CheckMemoryModuleInventoryAvailability() error = %v", err)
	}
	if !available {
		t.Fatal("CheckMemoryModuleInventoryAvailability() available = false, want true")
	}
}

func TestCheckMemoryModuleInventoryAvailabilityReportsMissingSources(t *testing.T) {
	restore := stubMemoryModuleCommands(t,
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

	available, err := CheckMemoryModuleInventoryAvailability(context.Background())
	if available {
		t.Fatal("CheckMemoryModuleInventoryAvailability() available = true, want false")
	}
	if err == nil || !strings.Contains(err.Error(), "dmidecode is not installed") {
		t.Fatalf("CheckMemoryModuleInventoryAvailability() error = %v", err)
	}
}

func stubMemoryModuleCommands(
	t *testing.T,
	run func(context.Context, string, ...string) ([]byte, error),
	lookPath func(string) (string, error),
) func() {
	t.Helper()
	originalRunCommand := memoryModulesRunCommand
	originalLookPath := memoryModulesLookPath
	memoryModulesRunCommand = run
	if lookPath != nil {
		memoryModulesLookPath = lookPath
	}
	return func() {
		memoryModulesRunCommand = originalRunCommand
		memoryModulesLookPath = originalLookPath
	}
}
