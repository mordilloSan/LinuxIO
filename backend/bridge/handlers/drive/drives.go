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
			"type":   dev.Tran,
			"vendor": strings.TrimSpace(dev.Vendor),
			"ro":     dev.RO,
		}

		// Add SMART info for all drives
		smart, err := FetchSmartInfo(dev.Name)
		if err != nil {
			drive["smartError"] = err.Error()
		} else {
			drive["smart"] = smart
		}

		// Add NVMe power info if NVMe
		if dev.Tran == "nvme" {
			power, err := GetNVMePowerState(dev.Name)
			if err != nil {
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

func FetchSmartInfo(device string) (map[string]any, error) {
	validName := regexp.MustCompile(`^(sd[a-z]|hd[a-z]|nvme\d+n\d+)$`)
	if !validName.MatchString(device) {
		return nil, errors.New("invalid device name")
	}

	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, fmt.Errorf("smartctl not found: %w", err)
	}

	cmd := exec.Command(smartctlPath, "--json", "-x", "/dev/"+device)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("smartctl failed: %w", err)
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
		return nil, fmt.Errorf("failed to run nvme id-ctrl: %w", err)
	}

	psRegex := regexp.MustCompile(`ps\s+(\d+)\s+:\s+mp:([\d.]+)W`)
	var states []PowerStateInfo
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if match := psRegex.FindStringSubmatch(line); len(match) == 3 {
			stateNum, _ := strconv.Atoi(match[1])
			maxPower, _ := strconv.ParseFloat(match[2], 64)
			states = append(states, PowerStateInfo{
				State:       stateNum,
				MaxPowerW:   maxPower,
				Description: strings.TrimSpace(line),
			})
		}
	}

	if len(states) == 0 {
		return nil, fmt.Errorf("no power states found for %s", device)
	}

	// Step 2: Get current power state
	cmd = exec.Command("nvme", "smart-log", "/dev/"+device)
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvme smart-log: %w", err)
	}
	stateRe := regexp.MustCompile(`Power State:\s+(\d+)`)
	match := stateRe.FindStringSubmatch(string(out))

	var currentState int
	var estimated float64

	if len(match) == 2 {
		currentState, _ = strconv.Atoi(match[1])
		for _, s := range states {
			if s.State == currentState {
				estimated = s.MaxPowerW
				break
			}
		}
	} else {
		currentState = -1
		if len(states) > 0 {
			estimated = states[0].MaxPowerW // fallback to first power state
		}
	}

	return &InferredPowerData{
		CurrentState: currentState,
		EstimatedW:   estimated,
		States:       states,
	}, nil
}
