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
	planSwapFileRow    = apischema.SwapFileRow{Path: "/swap.img", Active: true, InFstab: true, Size: 2 * gib, Used: 512 * mib}
	planCreate         = apischema.SwapFileCreate{Path: ownSwapFilePath, MinSize: swapFileMinSize, DefaultSize: 2 * gib, MaxSize: 45 * gib}
)

// planView builds what the planner reads for a host in the given mode: on
// disk, zswap and mixed it swaps on /swap.img from fstab; on zram and none a
// switch to zram removed it. LinuxIO can make its own swap file.
func planView(mode apischema.SwapMode) swapView {
	status := apischema.SwapStatus{
		ZramAvailable: true,
		Zswap:         apischema.ZswapStatus{Available: true, Compressor: "lzo"},
		Create:        planCreate,
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
	view := swapView{status: status}
	if slices.Contains(status.Devices, planSwapFileDevice) {
		view.status.SwapFiles = []apischema.SwapFileRow{planSwapFileRow}
		view.fstab = []fstabSwap{{path: "/swap.img", options: "sw"}}
	}
	return view
}

var (
	stepSysctls    = swapStep{code: apischema.SwapStepApplySysctls}
	stepRemoveFile = swapStep{code: apischema.SwapStepRemoveSwapFile, path: "/swap.img"}
	stepCreateFile = swapStep{code: apischema.SwapStepCreateSwapFile, path: ownSwapFilePath, size: 2 * gib}
)

func TestPlanSwapModeOrdersTheSteps(t *testing.T) {
	toZram := func(removes bool) []swapStep {
		steps := []swapStep{
			{code: apischema.SwapStepWriteConfig},
			{code: apischema.SwapStepStartZram},
			stepSysctls,
			{code: apischema.SwapStepDisableZswap},
		}
		if removes {
			steps = append(steps, stepRemoveFile)
		}
		return steps
	}
	toDiskSwap := func(last apischema.SwapStep, creates, zramRunning bool) []swapStep {
		var steps []swapStep
		if creates {
			steps = append(steps, stepCreateFile)
		}
		return append(steps, swapStep{code: apischema.SwapStepStopZram, run: zramRunning}, stepSysctls, swapStep{code: last})
	}
	tests := []struct {
		from, to apischema.SwapMode
		want     []swapStep
	}{
		{from: apischema.SwapModeNone, to: apischema.SwapModeZram, want: toZram(false)},
		{from: apischema.SwapModeDisk, to: apischema.SwapModeZram, want: toZram(true)},
		{from: apischema.SwapModeZswap, to: apischema.SwapModeZram, want: toZram(true)},
		{from: apischema.SwapModeZram, to: apischema.SwapModeZram, want: toZram(false)},
		{from: apischema.SwapModeMixed, to: apischema.SwapModeZram, want: toZram(true)},

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
			req := apischema.SwapModeRequest{Mode: test.to}
			if test.to == apischema.SwapModeZram {
				req.ConfirmRemove = removableSwapFiles(planView(test.from).status.SwapFiles)
			}
			got, err := planSwapMode(planView(test.from), req)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("steps =\n%+v\nwant\n%+v", got, test.want)
			}
		})
	}
}

// A switch to zram removes every swap file that passes the strict check, a
// temporary one first, and never one that does not.
func TestPlanSwapModeRemovesEveryRemovableSwapFile(t *testing.T) {
	view := planView(apischema.SwapModeDisk)
	view.status.SwapFiles = []apischema.SwapFileRow{
		planSwapFileRow,
		{Path: "/home/swapfile", Active: true, LockedReason: apischema.SwapFileNotInFstab},
		{Path: "/swap.img.linuxio-resize", Active: true, Temporary: true},
		{Path: "/old.img", InFstab: true, ResizeBlockedReason: apischema.SwapFileResizeOff},
	}
	confirm := []string{"/old.img", "/swap.img", "/swap.img.linuxio-resize"}
	got, err := planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmRemove: confirm})
	if err != nil {
		t.Fatal(err)
	}
	var removed []string
	for _, step := range got {
		if step.code == apischema.SwapStepRemoveSwapFile {
			removed = append(removed, step.path)
		}
	}
	if want := []string{"/swap.img.linuxio-resize", "/swap.img", "/old.img"}; !slices.Equal(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
}

// The request must name exactly the files the switch removes.
func TestPlanSwapModeRefusesAnUnconfirmedRemove(t *testing.T) {
	two := planView(apischema.SwapModeDisk)
	two.status.SwapFiles = append(two.status.SwapFiles, apischema.SwapFileRow{Path: "/swap2.img", Active: true, InFstab: true})
	tests := map[string]struct {
		view    swapView
		confirm []string
	}{
		"none confirmed":             {view: planView(apischema.SwapModeDisk)},
		"another file confirmed":     {view: planView(apischema.SwapModeDisk), confirm: []string{"/swapfile"}},
		"one of two confirmed":       {view: two, confirm: []string{"/swap.img"}},
		"one more than removed":      {view: two, confirm: []string{"/swap.img", "/swap2.img", "/swap3.img"}},
		"a file with none to remove": {view: planView(apischema.SwapModeZram), confirm: []string{"/swap.img"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := planSwapMode(test.view, apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmRemove: test.confirm})
			if !errors.Is(err, bridgeipc.ErrConflict) {
				t.Fatalf("plan = %+v, err = %v; want a conflict", got, err)
			}
		})
	}
	if _, err := planSwapMode(two, apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmRemove: []string{"/swap2.img", "/swap.img"}}); err != nil {
		t.Fatalf("both confirmed in another order: %v", err)
	}
}

// zram that zram-tools did not start is not stopped through zramswap.service.
func TestPlanSwapModeLeavesZramWithoutZramToolsAlone(t *testing.T) {
	view := planView(apischema.SwapModeMixed)
	view.status.ZramAvailable = false
	got, err := planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	want := []swapStep{stepSysctls, {code: apischema.SwapStepDisableZswap}}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %+v, want %+v", got, want)
	}
}

// A swap partition is disk swap enough for zswap and disk, and zram can be
// chosen beside it. No step ever names it.
func TestPlanSwapModeWithASwapPartition(t *testing.T) {
	view := planView(apischema.SwapModeNone)
	view.status.Devices = []apischema.SwapDevice{planPartition}
	got, err := planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeZswap})
	if err != nil {
		t.Fatal(err)
	}
	want := []swapStep{{code: apischema.SwapStepStopZram}, stepSysctls, {code: apischema.SwapStepEnableZswap}}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %+v, want %+v", got, want)
	}
	got, err = planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(got, func(s swapStep) bool { return s.path != "" }) {
		t.Fatalf("steps = %+v, want none that names a device", got)
	}
}

// The size of the swap file a switch makes is the request's, or the default.
func TestPlanSwapModeCreatesAtTheRequestedSize(t *testing.T) {
	got, err := planSwapMode(planView(apischema.SwapModeZram), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk, SwapFileSize: 4 * gib})
	if err != nil {
		t.Fatal(err)
	}
	if want := (swapStep{code: apischema.SwapStepCreateSwapFile, path: ownSwapFilePath, size: 4 * gib}); got[0] != want {
		t.Fatalf("first step = %+v, want %+v", got[0], want)
	}
}

func TestPlanSwapModeRefusals(t *testing.T) {
	tests := []struct {
		name   string
		target apischema.SwapMode
		view   func() swapView
		size   uint64
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
			name:   "disk while a swap file in fstab is off",
			target: apischema.SwapModeDisk,
			view: func() swapView {
				v := planView(apischema.SwapModeZram)
				v.status.SwapFiles = []apischema.SwapFileRow{{Path: "/swap.img", InFstab: true, ResizeBlockedReason: apischema.SwapFileResizeOff}}
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "zswap when LinuxIO cannot make its swap file",
			target: apischema.SwapModeZswap,
			view: func() swapView {
				v := planView(apischema.SwapModeZram)
				v.status.Create.BlockedReason = apischema.SwapFileCreateUnsupportedFilesystem
				return v
			},
			want: bridgeipc.ErrConflict,
		},
		{
			name:   "a swap file larger than the disk allows",
			target: apischema.SwapModeDisk,
			view:   func() swapView { return planView(apischema.SwapModeNone) },
			size:   46 * gib,
			want:   bridgeipc.ErrInvalidArgs,
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
			got, err := planSwapMode(test.view(), apischema.SwapModeRequest{Mode: test.target, SwapFileSize: test.size})
			if !errors.Is(err, test.want) {
				t.Fatalf("plan = %+v, err = %v; want %v", got, err, test.want)
			}
		})
	}
}

// A size that is not whole MiB or below the smallest is refused before the
// lock.
func TestValidateSwapModeRequestRefusesASwapFileSize(t *testing.T) {
	for _, size := range []uint64{gib + 1, 255 * mib} {
		if err := validateSwapModeRequest(apischema.SwapModeRequest{Mode: apischema.SwapModeDisk, SwapFileSize: size}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("size %d: err = %v, want invalid", size, err)
		}
	}
}
