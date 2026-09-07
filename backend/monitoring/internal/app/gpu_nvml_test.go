//go:build amd64 && glibc

package app

import (
	"testing"
	"unsafe"
)

func TestReadNVMLProcessesV3(t *testing.T) {
	fn := func(_ nvmlDevice, count *uint32, ptr uintptr) nvmlReturn {
		if ptr == 0 {
			*count = 2
			return nvmlReturn(nvmlErrorInsufficientSize)
		}
		items := unsafe.Slice((*nvmlProcessInfoV2)(unsafe.Pointer(ptr)), *count)
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
		items := unsafe.Slice((*nvmlProcessInfoV1)(unsafe.Pointer(ptr)), *count)
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
		items := unsafe.Slice((*nvmlProcessUtilization)(unsafe.Pointer(ptr)), *count)
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
