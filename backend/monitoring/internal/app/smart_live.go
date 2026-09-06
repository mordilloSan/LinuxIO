package app

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

var (
	nvmePowerTablePattern   = regexp.MustCompile(`ps\s+(\d+)\s+:\s+mp:([\d.]+)W`)
	nvmeCurrentValuePattern = regexp.MustCompile(`Current value:\s*(0x)?([0-9a-fA-F]+)`)
	liveNVMeDevicePattern   = regexp.MustCompile(`^nvme[0-9]+n[0-9]+$`)
	nvmePowerCommand        = exec.CommandContext
)

const livePowerRefreshInterval = 15 * time.Second

// currentLiveSmartData serves the existing typed SMART cache and refreshes
// NVMe power data at most once per 15 seconds. Power commands are serialized
// so concurrent live requests cannot stampede the controller.
func (sm *SmartManager) currentLiveSmartData(ctx context.Context) map[string]monitoringapi.LiveSmart {
	result := make(map[string]monitoringapi.LiveSmart)
	if sm == nil {
		return result
	}
	data := sm.GetCurrentData()

	sm.powerMu.Lock()
	defer sm.powerMu.Unlock()
	sm.refreshLivePower(ctx, data)

	for key, item := range data {
		deviceKey := liveNVMeDevice(item.DiskName)
		power := sm.powerCache[deviceKey]
		if power != nil {
			copyPower := *power
			copyPower.States = append([]monitoringapi.DiskPowerState(nil), power.States...)
			power = &copyPower
		}
		id := key
		if name := strings.TrimSpace(item.DiskName); name != "" {
			id = filepath.Base(name)
		}
		if id == "." || id == "" {
			id = key
		}
		result[id] = monitoringapi.LiveSmart{Data: item, Power: power}
	}
	return result
}

// refreshLivePower runs while powerMu is held by currentLiveSmartData.
func (sm *SmartManager) refreshLivePower(ctx context.Context, data map[string]monitoringapi.SmartData) {
	if sm.powerCache == nil {
		sm.powerCache = make(map[string]*monitoringapi.DiskPowerData)
	}
	if sm.powerAt.IsZero() || time.Since(sm.powerAt) >= livePowerRefreshInterval {
		for _, item := range data {
			if ctx.Err() != nil {
				break
			}
			device := liveNVMeDevice(item.DiskName)
			if device == "" {
				continue
			}
			power, err := getLiveNVMePowerState(ctx, device)
			if err == nil {
				sm.powerCache[device] = power
			}
		}
		// Record attempted refreshes too. A missing nvme utility is an
		// optional capability and must not be retried for every live request.
		if ctx.Err() == nil {
			sm.powerAt = time.Now()
		}
	}

}

func liveNVMeDevice(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	if !strings.HasPrefix(name, "nvme") || !liveNVMeDevicePattern.MatchString(name) {
		return ""
	}
	return name
}

func getLiveNVMePowerState(ctx context.Context, device string) (*monitoringapi.DiskPowerData, error) {
	if liveNVMeDevice(device) == "" {
		return nil, fmt.Errorf("invalid NVMe device %q", device)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := nvmePowerCommand(commandCtx, "nvme", "id-ctrl", "/dev/"+device).Output()
	if err != nil {
		return nil, fmt.Errorf("nvme id-ctrl %s: %w", device, err)
	}
	states := make([]monitoringapi.DiskPowerState, 0)
	for line := range strings.SplitSeq(string(out), "\n") {
		match := nvmePowerTablePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		state, stateErr := strconv.Atoi(match[1])
		watts, wattsErr := strconv.ParseFloat(match[2], 64)
		if stateErr != nil || wattsErr != nil {
			continue
		}
		states = append(states, monitoringapi.DiskPowerState{Description: strings.TrimSpace(line), MaxPowerW: watts, State: state})
	}
	if len(states) == 0 {
		return nil, fmt.Errorf("no power states found for %s", device)
	}

	currentState := -1
	estimated := states[0].MaxPowerW
	if feature, featureErr := nvmePowerCommand(commandCtx, "nvme", "get-feature", "/dev/"+device, "-f", "0x02").Output(); featureErr == nil {
		if match := nvmeCurrentValuePattern.FindStringSubmatch(string(feature)); len(match) == 3 {
			if state, parseErr := strconv.ParseUint(match[2], 16, 64); parseErr == nil {
				currentState = int(state & 0x1f)
			}
		}
	}
	for _, item := range states {
		if item.State == currentState {
			estimated = item.MaxPowerW
			break
		}
	}
	return &monitoringapi.DiskPowerData{CurrentState: currentState, EstimatedW: estimated, States: states}, nil
}
