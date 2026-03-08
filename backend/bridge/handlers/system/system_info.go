package system

import (
	"fmt"

	"github.com/jaypipes/ghw/pkg/bios"
	"github.com/jaypipes/ghw/pkg/chassis"
	"github.com/jaypipes/ghw/pkg/product"
	"github.com/shirou/gopsutil/v4/cpu"
)

type SystemInfo struct {
	ChassisType    string `json:"chassisType"`
	ProductName    string `json:"productName"`
	ProductVersion string `json:"productVersion"`
	ProductVendor  string `json:"productVendor"`
	BIOSVendor     string `json:"biosVendor"`
	BIOSVersion    string `json:"biosVersion"`
	BIOSDate       string `json:"biosDate"`
	CPUSummary     string `json:"cpuSummary"`
}

func FetchSystemInfo() (*SystemInfo, error) {
	info := &SystemInfo{}

	if ch, err := chassis.New(); err == nil {
		info.ChassisType = ch.TypeDescription
	}

	if pr, err := product.New(); err == nil {
		info.ProductName = pr.Name
		info.ProductVersion = pr.Version
		info.ProductVendor = pr.Vendor
	}

	if bi, err := bios.New(); err == nil {
		info.BIOSVendor = bi.Vendor
		info.BIOSVersion = bi.Version
		info.BIOSDate = bi.Date
	}

	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		counts, _ := cpu.Counts(true)
		info.CPUSummary = fmt.Sprintf("%dx %s", counts, cpuInfo[0].ModelName)
	}

	return info, nil
}
