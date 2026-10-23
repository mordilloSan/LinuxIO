package live

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

func TestSmartPowerSnapshotCachesPowerForFifteenSeconds(t *testing.T) {
	data := map[string]smartmodel.SmartData{
		"serial": {DiskName: "/dev/nvme0", SerialNumber: "serial"},
	}
	var power SmartPower
	var calls int
	original := nvmePowerCommand
	nvmePowerCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls++
		mode := "states"
		if strings.Contains(strings.Join(args, " "), "get-feature") {
			mode = "feature"
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperNVMePowerCommand", "--", mode)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "GORACE=atexit_sleep_ms=0")
		return cmd
	}
	t.Cleanup(func() { nvmePowerCommand = original })

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	power.Snapshot(canceledCtx, data)
	require.True(t, power.at.IsZero(), "a canceled request must not suppress the next refresh")
	require.Zero(t, calls)

	first := power.Snapshot(context.Background(), data)
	require.Contains(t, first, "nvme0n1")
	require.NotNil(t, first["nvme0n1"].Power)
	require.Equal(t, 1, first["nvme0n1"].Power.CurrentState)
	require.Equal(t, 2, calls)

	second := power.Snapshot(context.Background(), data)
	require.Equal(t, 2, calls)
	require.Equal(t, first["nvme0n1"].Power, second["nvme0n1"].Power)
}

func TestHelperNVMePowerCommand(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "states":
		fmt.Fprintln(os.Stdout, "ps 0 : mp:5W\nps 1 : mp:10W")
	case "feature":
		fmt.Fprintln(os.Stdout, "Current value:00000001")
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(2)
	}
	os.Exit(0)
}
