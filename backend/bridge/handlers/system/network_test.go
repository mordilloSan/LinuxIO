package system

import (
	"testing"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/stretchr/testify/require"
)

func TestFetchNetworksComputesRatesOnDemand(t *testing.T) {
	originalCounterSampler := netCounterSampler
	originalInterfaceReader := netInterfaceReader
	originalSpeedReader := netSpeedReader
	originalClock := netClock
	originalLastCounters := lastNetCounters
	originalLastSampleAt := lastNetSampleAt
	t.Cleanup(func() {
		netCounterSampler = originalCounterSampler
		netInterfaceReader = originalInterfaceReader
		netSpeedReader = originalSpeedReader
		netClock = originalClock
		lastNetCounters = originalLastCounters
		lastNetSampleAt = originalLastSampleAt
	})

	lastNetCounters = map[string]gopsnet.IOCountersStat{}
	lastNetSampleAt = time.Time{}

	samples := []map[string]gopsnet.IOCountersStat{
		{
			"eth0": {Name: "eth0", BytesRecv: 1024, BytesSent: 2048},
			"lo":   {Name: "lo", BytesRecv: 4096, BytesSent: 4096},
		},
		{
			"eth0": {Name: "eth0", BytesRecv: 3072, BytesSent: 6144},
			"lo":   {Name: "lo", BytesRecv: 8192, BytesSent: 8192},
		},
	}
	sampleIndex := 0
	netCounterSampler = func() map[string]gopsnet.IOCountersStat {
		sample := samples[sampleIndex]
		sampleIndex++
		return sample
	}

	netInterfaceReader = func() ([]gopsnet.InterfaceStat, error) {
		return []gopsnet.InterfaceStat{
			{
				Name:         "eth0",
				HardwareAddr: "00:11:22:33:44:55",
				Addrs:        []gopsnet.InterfaceAddr{{Addr: "192.168.1.10/24"}},
			},
			{
				Name:         "lo",
				HardwareAddr: "",
				Addrs:        []gopsnet.InterfaceAddr{{Addr: "127.0.0.1/8"}},
			},
		}, nil
	}

	netSpeedReader = func(name string) string {
		require.Equal(t, "eth0", name)
		return "1000 Mbps"
	}

	timestamps := []time.Time{
		time.Unix(100, 0),
		time.Unix(101, 0),
	}
	timestampIndex := 0
	netClock = func() time.Time {
		ts := timestamps[timestampIndex]
		timestampIndex++
		return ts
	}

	first, err := FetchNetworks()
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "eth0", first[0].Name)
	require.Equal(t, []string{"192.168.1.10/24"}, first[0].IPv4)
	require.Equal(t, "00:11:22:33:44:55", first[0].MAC)
	require.Equal(t, "1000 Mbps", first[0].Speed)
	require.Zero(t, first[0].RxKBs)
	require.Zero(t, first[0].TxKBs)

	second, err := FetchNetworks()
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, 2.0, second[0].RxKBs)
	require.Equal(t, 4.0, second[0].TxKBs)
}

func TestComputeSimpleNetRatesReturnsZeroForInvalidSamples(t *testing.T) {
	previous := map[string]gopsnet.IOCountersStat{
		"eth0": {BytesRecv: 200, BytesSent: 400},
	}
	current := map[string]gopsnet.IOCountersStat{
		"eth0": {BytesRecv: 100, BytesSent: 300},
	}

	rx, tx := computeSimpleNetRates("eth0", previous, current, 1)
	require.Zero(t, rx)
	require.Zero(t, tx)

	rx, tx = computeSimpleNetRates("missing", previous, current, 1)
	require.Zero(t, rx)
	require.Zero(t, tx)

	rx, tx = computeSimpleNetRates("eth0", previous, current, 0)
	require.Zero(t, rx)
	require.Zero(t, tx)

	validCurrent := map[string]gopsnet.IOCountersStat{
		"eth0": {BytesRecv: 1224, BytesSent: 2450},
	}
	rx, tx = computeSimpleNetRates("eth0", previous, validCurrent, 1)
	require.Equal(t, 1.0, rx)
	require.Equal(t, 2.001953125, tx)
}
