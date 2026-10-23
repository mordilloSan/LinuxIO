package system

// Counters are the raw readings behind the rounded figures of Stats. Collectors
// fill them for the live payload; they are not part of the persisted schema.
type Counters struct {
	MemoryBytes MemoryBytes
	DiskDevices map[string]DiskDeviceRates
	// NetworkInterfaceCounters preserves the cumulative kernel counters for
	// the live API; the aggregate payload only retains byte totals.
	NetworkInterfaceCounters map[string]NetworkInterfaceCounters
}

// MemoryBytes carries the raw memory counters behind the rounded gigabyte
// fields above.
type MemoryBytes struct {
	Total, Used, Available, Free, Cached, Buffers, Shared, SwapTotal, SwapFree, ZFSArc uint64
}

// DiskDeviceRates carries per-block-device I/O rates.
type DiskDeviceRates struct {
	ReadBytesPerSec, WriteBytesPerSec, ReadOpsPerSec, WriteOpsPerSec float64
}

type NetworkInterfaceCounters struct {
	RxDropped, RxErrors, RxPackets uint64
	TxDropped, TxErrors, TxPackets uint64
}
