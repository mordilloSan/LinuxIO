//go:build amd64 && glibc

package gpu

import (
	"slices"
	"testing"
	"unsafe"
)

// nvmlBuffer turns the address a reader hands NVML back into the slice it
// points to, as the C library does. checkptr cannot know that the address came
// from a live Go slice and refuses the conversion under -race, so it is off
// here.
//
//go:nocheckptr
func nvmlBuffer[T any](ptr uintptr, count uint32) []T {
	return unsafe.Slice((*T)(unsafe.Pointer(ptr)), count)
}

func TestReadNVMLProcessesV3(t *testing.T) {
	fn := func(_ nvmlDevice, count *uint32, ptr uintptr) nvmlReturn {
		if ptr == 0 {
			*count = 2
			return nvmlReturn(nvmlErrorInsufficientSize)
		}
		items := nvmlBuffer[nvmlProcessInfoV2](ptr, *count)
		items[0].PID = 42
		items[0].UsedGpuMemory = 4096
		items[1].PID = 43
		items[1].UsedGpuMemory = nvmlValueNotAvailable
		return nvmlReturn(nvmlSuccess)
	}
	got := readNVMLProcesses(0, fn, 3)
	if len(got) != 2 || got[0].pid != 42 || got[0].memory == nil || *got[0].memory != 4096 || got[1].memory != nil {
		t.Fatalf("unexpected process samples: %#v", got)
	}
}

func TestNVMLProcessABISizes(t *testing.T) {
	if got := unsafe.Sizeof(nvmlProcessInfoV1{}); got != 16 {
		t.Fatalf("nvmlProcessInfoV1 size = %d, want 16", got)
	}
	if got := unsafe.Sizeof(nvmlProcessInfoV2{}); got != 24 {
		t.Fatalf("nvmlProcessInfoV2 size = %d, want 24", got)
	}
	if got := unsafe.Sizeof(nvmlPciInfo{}); got != 68 {
		t.Fatalf("nvmlPciInfo size = %d, want 68 (v3 layout, the largest ABI)", got)
	}
	if got := unsafe.Sizeof(nvmlProcessUtilization{}); got != 32 {
		t.Fatalf("nvmlProcessUtilization size = %d, want 32", got)
	}
}

func TestReadNVMLProcessesV1(t *testing.T) {
	fn := func(_ nvmlDevice, count *uint32, ptr uintptr) nvmlReturn {
		if ptr == 0 {
			*count = 1
			return nvmlReturn(nvmlErrorInsufficientSize)
		}
		items := nvmlBuffer[nvmlProcessInfoV1](ptr, *count)
		items[0].PID = 7
		items[0].UsedGpuMemory = 128
		return nvmlReturn(nvmlSuccess)
	}
	got := readNVMLProcesses(0, fn, 1)
	if len(got) != 1 || got[0].pid != 7 || got[0].memory == nil || *got[0].memory != 128 {
		t.Fatalf("unexpected v1 process samples: %#v", got)
	}
}

func TestReadNVMLProcessUtilizationUsesNewestSample(t *testing.T) {
	fn := func(_ nvmlDevice, ptr uintptr, count *uint32, lastSeen uint64) nvmlReturn {
		if lastSeen != 100 {
			t.Fatalf("lastSeen = %d, want 100", lastSeen)
		}
		if ptr == 0 {
			*count = 3
			return nvmlReturn(nvmlErrorInsufficientSize)
		}
		items := nvmlBuffer[nvmlProcessUtilization](ptr, *count)
		items[0] = nvmlProcessUtilization{PID: 42, TimeStamp: 110, SMUtil: 10}
		items[1] = nvmlProcessUtilization{PID: 42, TimeStamp: 120, SMUtil: 20}
		items[2] = nvmlProcessUtilization{PID: 43, TimeStamp: 115, SMUtil: 30}
		return nvmlReturn(nvmlSuccess)
	}

	got, nextLastSeen := readNVMLProcessUtilization(0, fn, 100)
	if nextLastSeen != 120 || len(got) != 2 || got[0].PID != 42 || got[0].SMUtil != 20 || got[1].PID != 43 {
		t.Fatalf("unexpected utilization samples: next=%d items=%#v", nextLastSeen, got)
	}
}

func TestReadNVMLProcessesIgnoresFailures(t *testing.T) {
	fn := func(_ nvmlDevice, count *uint32, _ uintptr) nvmlReturn { *count = 0; return nvmlReturn(3) }
	if got := readNVMLProcesses(0, fn, 1); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestNVMLPciInfoSymbol(t *testing.T) {
	tests := []struct {
		name    string
		exports []string
		want    string
	}{
		{
			name:    "prefers v3",
			exports: []string{"nvmlDeviceGetPciInfo", "nvmlDeviceGetPciInfo_v2", "nvmlDeviceGetPciInfo_v3"},
			want:    "nvmlDeviceGetPciInfo_v3",
		},
		{
			name:    "falls back to the unversioned symbol",
			exports: []string{"nvmlDeviceGetPciInfo", "nvmlDeviceGetPciInfo_v2"},
			want:    "nvmlDeviceGetPciInfo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			has := func(symbol string) bool { return slices.Contains(tt.exports, symbol) }
			if got := nvmlPciInfoSymbol(has); got != tt.want {
				t.Fatalf("nvmlPciInfoSymbol() = %q, want %q", got, tt.want)
			}
		})
	}
}
