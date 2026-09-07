//go:build !amd64 || !glibc

package app

import (
	"context"
	"fmt"
)

type nvmlCollector struct {
	gm          *GPUManager
	processOnly bool
}

func (c *nvmlCollector) init() error {
	return fmt.Errorf("nvml not supported on this platform")
}

func (c *nvmlCollector) start(_ context.Context) {}

func (c *nvmlCollector) startProcesses(_ context.Context) {}

func detectNVMLAvailability() bool {
	return false
}
