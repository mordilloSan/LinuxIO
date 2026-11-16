package memory

import (
	"errors"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

var (
	planSwapFileDevice = apischema.SwapDevice{Path: "/swap.img", Kind: apischema.SwapDeviceFile, Size: 2 * gib, Used: 512 * mib, Priority: -2}
	planZramDevice     = apischema.SwapDevice{Path: "/dev/zram0", Kind: apischema.SwapDeviceZram, Size: 8 * gib, Used: 256 * mib, Priority: 100}
	planPartition      = apischema.SwapDevice{Path: "/dev/sda3", Kind: apischema.SwapDevicePartition, Size: 8 * gib, Priority: -3}
)

// planView builds what the planner reads for a host whose fstab declares
// /swap.img, in the given mode. A host in mode none has no active device.
func planView(mode apischema.SwapMode) swapView {
	status := apischema.SwapStatus{
		ZramAvailable: true,
		Zswap:         apischema.ZswapStatus{Available: true, Compressor: "lzo"},
		SwapFile:      &apischema.SwapFile{Path: "/swap.img"},
	}
	switch mode {
	case apischema.SwapModeDisk:
		status.Devices = []apischema.SwapDevice{planSwapFileDevice}
	case apischema.SwapModeZswap:
		status.Devices = []apischema.SwapDevice{planSwapFileDevice}
		status.Zswap.Enabled = true
	case apischema.SwapModeZram:
		status.Devices = []apischema.SwapDevice{planZramDevice}
	case apischema.SwapModeMixed:
		status.Devices = []apischema.SwapDevice{planZramDevice, planSwapFileDevice}
	default:
	}
	status.Mode = mode
	entry := fstabSwap{path: "/swap.img", unit: "swap.img.swap"}
	if slices.Contains(status.Devices, planSwapFileDevice) {
		entry.device = "/swap.img"
		status.SwapFile.Active = true
	}
	return swapView{status: status, fstab: []fstabSwap{entry}}
}

var (
	stepStopFile  = swapStep{code: apischema.SwapStepStopSwapFile, unit: "swap.img.swap", path: "/swap.img"}
	stepStartFile = swapStep{code: apischema.SwapStepStartSwapFile, unit: "swap.img.swap", path: "/swap.img"}
	stepSysctls   = swapStep{code: apischema.SwapStepApplySysctls}
)

func TestPlanSwapModeOrdersTheSteps(t *testing.T) {
	toZram := func() []swapStep {
		return []swapStep{
			{code: apischema.SwapStepWriteConfig},
			{code: apischema.SwapStepStartZram},
			stepSysctls,
			{code: apischema.SwapStepDisableZswap},
			stepStopFile,
		}
	}
	toDiskSwap := func(last apischema.SwapStep, fileStopped, zramRunning bool) []swapStep {
		var steps []swapStep
		if fileStopped {
			steps = append(steps, stepStartFile)
		}
		return append(steps, swapStep{code: apischema.SwapStepStopZram, run: zramRunning}, stepSysctls, swapStep{code: last})
	}
	tests := []struct {
		from, to apischema.SwapMode
		want     []swapStep
	}{
		{from: apischema.SwapModeNone, to: apischema.SwapModeZram, want: toZram()},
		{from: apischema.SwapModeDisk, to: apischema.SwapModeZram, want: toZram()},
		{from: apischema.SwapModeZswap, to: apischema.SwapModeZram, want: toZram()},
		{from: apischema.SwapModeZram, to: apischema.SwapModeZram, want: toZram()},
		{from: apischema.SwapModeMixed, to: apischema.SwapModeZram, want: toZram()},

		{from: apischema.SwapModeNone, to: apischema.SwapModeZswap, want: toDiskSwap(apischema.SwapStepEnableZswap, true, false)},
		{from: apischema.SwapModeDisk, to: apischema.SwapModeZswap, want: toDiskSwap(apischema.SwapStepEnableZswap, false, false)},
		{from: apischema.SwapModeZswap, to: apischema.SwapModeZswap, want: toDiskSwap(apischema.SwapStepEnableZswap, false, false)},
		{from: apischema.SwapModeZram, to: apischema.SwapModeZswap, want: toDiskSwap(apischema.SwapStepEnableZswap, true, true)},
		{from: apischema.SwapModeMixed, to: apischema.SwapModeZswap, want: toDiskSwap(apischema.SwapStepEnableZswap, false, true)},

		{from: apischema.SwapModeNone, to: apischema.SwapModeDisk, want: toDiskSwap(apischema.SwapStepDisableZswap, true, false)},
		{from: apischema.SwapModeDisk, to: apischema.SwapModeDisk, want: toDiskSwap(apischema.SwapStepDisableZswap, false, false)},
		{from: apischema.SwapModeZswap, to: apischema.SwapModeDisk, want: toDiskSwap(apischema.SwapStepDisableZswap, false, false)},
		{from: apischema.SwapModeZram, to: apischema.SwapModeDisk, want: toDiskSwap(apischema.SwapStepDisableZswap, true, true)},
		{from: apischema.SwapModeMixed, to: apischema.SwapModeDisk, want: toDiskSwap(apischema.SwapStepDisableZswap, false, true)},
	}
	for _, test := range tests {
		t.Run(string(test.from)+" to "+string(test.to), func(t *testing.T) {
			got, err := planSwapMode(planView(test.from), test.to)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("steps =\n%+v\nwant\n%+v", got, test.want)
			}
		})
	}
}

// Only a swap file fstab declares has a unit to stop, mask or start: an
// active swap file without an fstab entry is left alone.
func TestPlanSwapModeLeavesASwapFileOutsideFstabAlone(t *testing.T) {
	view := planView(apischema.SwapModeDisk)
	view.fstab = nil
	got, err := planSwapMode(view, apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(got, func(s swapStep) bool { return s.code == apischema.SwapStepStopSwapFile }) {
		t.Fatalf("steps = %+v, want no swap file step", got)
	}
}

// zram that zram-tools did not start is not stopped through zramswap.service.
func TestPlanSwapModeLeavesZramWithoutZramToolsAlone(t *testing.T) {
	view := planView(apischema.SwapModeMixed)
	view.status.ZramAvailable = false
	got, err := planSwapMode(view, apischema.SwapModeDisk)
	if err != nil {
		t.Fatal(err)
	}
	want := []swapStep{stepSysctls, {code: apischema.SwapStepDisableZswap}}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %+v, want %+v", got, want)
	}
}

// A swap partition is disk swap enough for zswap and disk; it is never
// started, stopped or masked.
func TestPlanSwapModeWithOnlyASwapPartition(t *testing.T) {
	view := planView(apischema.SwapModeNone)
	view.status.SwapFile = nil
	view.fstab = nil
	view.status.Devices = []apischema.SwapDevice{planPartition}
	got, err := planSwapMode(view, apischema.SwapModeZswap)
	if err != nil {
		t.Fatal(err)
	}
	want := []swapStep{{code: apischema.SwapStepStopZram}, stepSysctls, {code: apischema.SwapStepEnableZswap}}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %+v, want %+v", got, want)
	}
}

func TestPlanSwapModeRefusals(t *testing.T) {
	tests := []struct {
		name   string
		target apischema.SwapMode
		view   func() swapView
		want   error
	}{
		{
			name:   "zram without zram-tools",
			target: apischema.SwapModeZram,
			view: func() swapView {
				v := planView(apischema.SwapModeDisk)
				v.status.ZramAvailable = false
				v.status.ZramUnavailableReason = apischema.ZramPackageMissing
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "zram beside an active swap partition",
			target: apischema.SwapModeZram,
			view: func() swapView {
				v := planView(apischema.SwapModeDisk)
				v.status.Devices = append(v.status.Devices, planPartition)
				v.status.ZramUnavailableReason = apischema.ZramSwapPartitionActive
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "zswap without a swap file or partition",
			target: apischema.SwapModeZswap,
			view: func() swapView {
				v := planView(apischema.SwapModeZram)
				v.status.SwapFile = nil
				v.fstab = nil
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "disk without a swap file or partition",
			target: apischema.SwapModeDisk,
			view: func() swapView {
				v := planView(apischema.SwapModeNone)
				v.status.SwapFile = nil
				v.fstab = nil
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "zswap on a kernel without zswap",
			target: apischema.SwapModeZswap,
			view: func() swapView {
				v := planView(apischema.SwapModeDisk)
				v.status.Zswap = apischema.ZswapStatus{}
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "unknown mode",
			target: "mixed",
			view:   func() swapView { return planView(apischema.SwapModeDisk) },
			want:   bridgeipc.ErrInvalidArgs,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := planSwapMode(test.view(), test.target)
			if !errors.Is(err, test.want) {
				t.Fatalf("plan = %+v, err = %v; want %v", got, err, test.want)
			}
		})
	}
}
