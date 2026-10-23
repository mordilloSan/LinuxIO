package storage

import (
	"context"
	"encoding/json"
	"fmt"
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
	Rota       bool          `json:"rota"`
	Type       string        `json:"type"`
	Tran       string        `json:"tran"`
	Vendor     string        `json:"vendor"`
	Mountpoint string        `json:"mountpoint"`
	Children   []BlockDevice `json:"children,omitempty"`
}

func FetchDriveInfo(ctx context.Context) ([]DriveInfo, error) {
	// Human-readable sizes (-O) are what the drive cards show, so this stays a
	// separate lsblk query from the byte-based topology tree.
	out, err := runLsblk(ctx, "-d", "-O", "-J")
	if err != nil {
		return nil, err
	}

	var parsed LSBLKOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse lsblk output: %w", err)
	}

	drives := make([]DriveInfo, 0, len(parsed.BlockDevices))
	for _, dev := range parsed.BlockDevices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if dev.Type != "disk" {
			continue
		}
		drives = append(drives, buildDriveInfo(dev))
	}

	return drives, nil
}

func buildDriveInfo(dev BlockDevice) DriveInfo {
	return DriveInfo{
		Name:       dev.Name,
		Model:      strings.TrimSpace(dev.Model),
		Serial:     optionalString(strings.TrimSpace(dev.Serial)),
		Size:       dev.Size,
		Type:       optionalString(dev.Tran),
		Vendor:     optionalString(strings.TrimSpace(dev.Vendor)),
		RO:         dev.RO,
		Rotational: dev.Rota,
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
