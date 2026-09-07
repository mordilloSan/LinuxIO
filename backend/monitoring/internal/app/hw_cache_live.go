package app

import (
	"log/slog"
	"sync"

	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/gpu"
)

// hwSnapshotCache caches ghw snapshots whose construction parses the PCI IDs
// database (~250ms of CPU per call). A successful topology snapshot is reused
// until the daemon restarts; failures are retried on the next call. The mutex
// is held across load so concurrent first callers parse the database once.
type hwSnapshotCache[T any] struct {
	mu    sync.Mutex
	value T
	ok    bool
}

func (c *hwSnapshotCache[T]) get(load func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok {
		return c.value, nil
	}
	value, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	c.value = value
	c.ok = true
	return value, nil
}

// Snapshots are read-only after load; handlers must not mutate them.
var (
	gpuInfoCache hwSnapshotCache[*gpu.Info]
)

func cachedGPUInfo() (*gpu.Info, error) {
	return gpuInfoCache.get(func() (*gpu.Info, error) {
		return gpu.New(ghw.WithLogger(slog.Default()))
	})
}
