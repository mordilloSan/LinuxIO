package storage

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// FetchDriveInfo returns the disk inventory read from sysfs and udev. Live
// SMART and power data belongs to the monitoring daemon; self-test mutations
// remain here.
func FetchDriveInfo(ctx context.Context) ([]apischema.ApiDisk, error) {
	devices, err := readBlockDevices(ctx)
	if err != nil {
		return nil, err
	}
	drives := make([]apischema.ApiDisk, 0, len(devices))
	for _, d := range devices {
		if d.Type != "disk" {
			continue
		}
		drives = append(drives, apischema.ApiDisk{
			Name:       d.Name,
			Model:      d.Model,
			Serial:     optionalString(d.Serial),
			SizeBytes:  d.SizeBytes,
			Type:       optionalString(d.Transport),
			Vendor:     optionalString(d.vendor),
			RO:         d.ReadOnly,
			Rotational: d.rotational,
		})
	}
	return drives, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
