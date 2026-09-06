package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

func TestCurrentLiveSmartDataCachesPowerForFifteenSeconds(t *testing.T) {
	manager := &SmartManager{SmartDataMap: map[string]*smart.SmartData{
		"serial": {DiskName: "/dev/nvme0n1", SerialNumber: "serial"},
	}}
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
	manager.currentLiveSmartData(canceledCtx)
	require.True(t, manager.powerAt.IsZero(), "a canceled request must not suppress the next refresh")
	require.Zero(t, calls)

	first := manager.currentLiveSmartData(context.Background())
	require.Contains(t, first, "nvme0n1")
	require.NotNil(t, first["nvme0n1"].Power)
	require.Equal(t, 1, first["nvme0n1"].Power.CurrentState)
	require.Equal(t, 2, calls)

	second := manager.currentLiveSmartData(context.Background())
	require.Equal(t, 2, calls)
	require.Equal(t, first["nvme0n1"].Power, second["nvme0n1"].Power)
}

func TestSmartParsersPreserveLiveHealthAndSelfTests(t *testing.T) {
	ataOutput, err := os.ReadFile(filepath.Join("testdata", "smart", "sda.json"))
	require.NoError(t, err)
	manager := &SmartManager{SmartDataMap: make(map[string]*smart.SmartData)}
	valid, _ := manager.parseSmartForSata(ataOutput)
	require.True(t, valid)
	ata := manager.SmartDataMap["9C40918040082"]
	require.NotEmpty(t, ata.Attributes)
	require.NotNil(t, ata.AtaSmartSelfTestLog)
	require.NotNil(t, ata.AtaSmartSelfTestLog.Standard)
	require.Len(t, ata.AtaSmartSelfTestLog.Standard.Table, 3)
	require.NotNil(t, ata.PowerOnTime)
	require.Equal(t, uint64(7344), ata.PowerOnTime.Hours)
	require.NotNil(t, ata.PowerCycleCount)
	require.Equal(t, uint64(104), *ata.PowerCycleCount)

	nvmeOutput, err := os.ReadFile(filepath.Join("testdata", "smart", "nvme0.json"))
	require.NoError(t, err)
	manager = &SmartManager{SmartDataMap: make(map[string]*smart.SmartData)}
	valid, _ = manager.parseSmartForNvme(nvmeOutput)
	require.True(t, valid)
	nvme := manager.SmartDataMap["2024031600129"]
	require.NotNil(t, nvme.NVMeSmartHealthInformationLog)
	require.Equal(t, uint32(4399), nvme.NVMeSmartHealthInformationLog.PowerOnHours)
	require.NotNil(t, nvme.NVMeSelfTestLog)
	require.NotNil(t, nvme.PowerOnTime)
	require.Equal(t, uint64(4399), nvme.PowerOnTime.Hours)
	require.NotNil(t, nvme.PowerCycleCount)
	require.Equal(t, uint64(430), *nvme.PowerCycleCount)

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
