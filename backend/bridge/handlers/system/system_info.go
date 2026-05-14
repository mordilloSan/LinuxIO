package system

import (
	"context"
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

func FetchCPUSummary(ctx context.Context) string {
	cpuInfo, err := cpu.InfoWithContext(ctx)
	if err != nil || len(cpuInfo) == 0 {
		return ""
	}

	counts, _ := cpu.CountsWithContext(ctx, true)
	if counts > 0 {
		return fmt.Sprintf("%dx %s", counts, cpuInfo[0].ModelName)
	}
	return cpuInfo[0].ModelName
}

func FetchSystemInfo(ctx context.Context) (*SystemInfo, error) {
	info := &SystemInfo{}

	// ghw has no context support; check ctx before each hardware metadata read.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ch, err := chassis.New(); err == nil {
		info.ChassisType = ch.TypeDescription
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pr, err := product.New(); err == nil {
		info.ProductName = pr.Name
		info.ProductVersion = pr.Version
		info.ProductVendor = pr.Vendor
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bi, err := bios.New(); err == nil {
		info.BIOSVendor = bi.Vendor
		info.BIOSVersion = bi.Version
		info.BIOSDate = bi.Date
	}

	info.CPUSummary = FetchCPUSummary(ctx)

	return info, nil
}
