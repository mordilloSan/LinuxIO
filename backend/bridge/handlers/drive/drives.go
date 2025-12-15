package drive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type LSBLKOutput struct {
	BlockDevices []BlockDevice `json:"blockdevices"`
}

type BlockDevice struct {
	Name       string        `json:"name"`
	Model      string        `json:"model"`
	Serial     string        `json:"serial"`
	Size       string        `json:"size"`
	RO         bool          `json:"ro"`
	Type       string        `json:"type"`
	Tran       string        `json:"tran"`
	Vendor     string        `json:"vendor"`
	Mountpoint string        `json:"mountpoint"`
	Children   []BlockDevice `json:"children,omitempty"`
}

type PowerStateInfo struct {
	State       int
	MaxPowerW   float64
	Description string
}

type InferredPowerData struct {
	CurrentState int              `json:"currentState"`
	EstimatedW   float64          `json:"estimatedW"`
	States       []PowerStateInfo `json:"states"`
}

// precompiled regexes
var (
	validDeviceNameRe = regexp.MustCompile(`^(sd[a-z]|hd[a-z]|nvme\d+n\d+)$`)
	nvmePsRe          = regexp.MustCompile(`ps\s+(\d+)\s+:\s+mp:([\d.]+)W`)
	nvmeStateRe       = regexp.MustCompile(`Power State:\s+(\d+)`)
)

func FetchDriveInfo() ([]map[string]any, error) {
	out, err := exec.Command("lsblk", "-d", "-O", "-J").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute lsblk: %w", err)
	}

	var parsed LSBLKOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse lsblk output: %w", err)
	}

	var drives []map[string]any
	for _, dev := range parsed.BlockDevices {
		if dev.Type != "disk" {
			continue
		}

		drive := map[string]any{
			"name":   dev.Name,
			"model":  strings.TrimSpace(dev.Model),
			"serial": strings.TrimSpace(dev.Serial),
			"size":   dev.Size,
			"type":   dev.Tran, // transport (sata, nvme, usb, etc)
			"vendor": strings.TrimSpace(dev.Vendor),
			"ro":     dev.RO,
		}

		// SMART info (best-effort)
		if smart, err := FetchSmartInfo(dev.Name); err != nil {
			drive["smartError"] = err.Error()
		} else {
			drive["smart"] = smart
		}

		// NVMe power info if it's an NVMe device
		if isNVMeDevice(dev) {
			if power, err := GetNVMePowerState(dev.Name); err != nil {
				drive["powerError"] = err.Error()
			} else {
				var states []map[string]any
				for _, s := range power.States {
					states = append(states, map[string]any{
						"state":       s.State,
						"maxPowerW":   s.MaxPowerW,
						"description": s.Description,
					})
				}
				drive["power"] = map[string]any{
					"currentState": power.CurrentState,
					"estimatedW":   power.EstimatedW,
					"states":       states,
				}
			}
		}

		drives = append(drives, drive)
	}

	return drives, nil
}

func isNVMeDevice(dev BlockDevice) bool {
	// On some systems lsblk tran for NVMe may not be "nvme", so also check the name.
	if strings.HasPrefix(dev.Name, "nvme") {
		return true
	}
	return dev.Tran == "nvme"
}

func FetchSmartInfo(device string) (map[string]any, error) {
	if !validDeviceNameRe.MatchString(device) {
		return nil, errors.New("invalid device name")
	}

	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, fmt.Errorf("smartctl not found: %w", err)
	}

	cmd := exec.Command(smartctlPath, "--json", "-x", "/dev/"+device)
	out, err := cmd.Output()
	if err != nil {
		// smartctl returns non-zero if drive doesn't support SMART, etc.
		// We still try to use whatever JSON it produced, but wrap the error.
		return nil, fmt.Errorf("smartctl failed for %s: %w", device, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse smartctl output: %w", err)
	}

	return parsed, nil
}

func GetNVMePowerState(device string) (*InferredPowerData, error) {
	// Step 1: Get supported power states
	cmd := exec.Command("nvme", "id-ctrl", "/dev/"+device)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvme id-ctrl for %s: %w", device, err)
	}

	var states []PowerStateInfo
	for _, line := range strings.Split(string(out), "\n") {
		match := nvmePsRe.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}

		stateNum, parseErr := strconv.Atoi(match[1])
		if parseErr != nil {
			continue
		}
		maxPower, powerErr := strconv.ParseFloat(match[2], 64)
		if powerErr != nil {
			continue
		}

		states = append(states, PowerStateInfo{
			State:       stateNum,
			MaxPowerW:   maxPower,
			Description: strings.TrimSpace(line),
		})
	}

	if len(states) == 0 {
		return nil, fmt.Errorf("no power states found for %s", device)
	}

	// Step 2: Get current power state
	cmd = exec.Command("nvme", "smart-log", "/dev/"+device)
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvme smart-log for %s: %w", device, err)
	}

	match := nvmeStateRe.FindStringSubmatch(string(out))

	currentState := -1
	var estimated float64

	if len(match) == 2 {
		if s, err := strconv.Atoi(match[1]); err == nil {
			currentState = s
			for _, ps := range states {
				if ps.State == currentState {
					estimated = ps.MaxPowerW
					break
				}
			}
		}
	}

	// Fallback: if we couldn't determine current state, use the first state's power as a rough estimate
	if currentState == -1 && len(states) > 0 && estimated == 0 {
		estimated = states[0].MaxPowerW
	}

	return &InferredPowerData{
		CurrentState: currentState,
		EstimatedW:   estimated,
		States:       states,
	}, nil
}
