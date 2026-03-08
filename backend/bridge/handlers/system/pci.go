package system

import (
	"fmt"

	"github.com/jaypipes/ghw/pkg/pci"
)

type PCIDevice struct {
	Class  string `json:"class"`
	Model  string `json:"model"`
	Vendor string `json:"vendor"`
	Slot   string `json:"slot"`
}

func FetchPCIDevices() ([]PCIDevice, error) {
	info, err := pci.New()
	if err != nil || info == nil {
		return nil, fmt.Errorf("failed to retrieve PCI information: %w", err)
	}

	devices := make([]PCIDevice, 0, len(info.Devices))
	for _, dev := range info.Devices {
		d := PCIDevice{
			Slot: dev.Address,
		}
		if dev.Class != nil {
			d.Class = dev.Class.Name
		}
		if dev.Product != nil {
			d.Model = pciName(dev.Product.Name, dev.Product.VendorID, dev.Product.ID)
		}
		if dev.Vendor != nil {
			d.Vendor = pciName(dev.Vendor.Name, "", dev.Vendor.ID)
		}
		devices = append(devices, d)
	}

	return devices, nil
}

// pciName returns the human-readable name, or a hex ID fallback if the name
// is "unknown" or empty. For products, vendorID:productID (e.g. "8086:461C").
// For vendors, just the ID.
func pciName(name, vendorID, id string) string {
	if name != "" && name != "unknown" {
		return name
	}
	if vendorID != "" {
		return vendorID + ":" + id
	}
	return id
}
