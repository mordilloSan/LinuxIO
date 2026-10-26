package storage

import "context"

func FetchDriveInfo(ctx context.Context) ([]DriveInfo, error) {
	devices, err := readBlockDevices(ctx)
	if err != nil {
		return nil, err
	}
	drives := make([]DriveInfo, 0, len(devices))
	for _, d := range devices {
		if d.Type != "disk" {
			continue
		}
		drives = append(drives, DriveInfo{
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
