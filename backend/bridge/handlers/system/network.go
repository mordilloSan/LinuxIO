// bridge/handlers/system/network.go
package system

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

type SimpleNetInfo struct {
	Name  string   `json:"name"`
	IPv4  []string `json:"ipv4"`
	MAC   string   `json:"mac"`
	Speed string   `json:"speed"`
	TxKBs float64  `json:"tx_speed"`
	RxKBs float64  `json:"rx_speed"`
}

var (
	netRateStateLock sync.Mutex
	lastNetCounters  = map[string]gopsnet.IOCountersStat{}
	lastNetSampleAt  time.Time

	netCounterSampler  = sampleIOCounters
	netInterfaceReader = func(ctx context.Context) ([]gopsnet.InterfaceStat, error) {
		return gopsnet.InterfacesWithContext(ctx)
	}
	netSpeedReader = readInterfaceSpeed
	netClock       = time.Now
)

func sampleIOCounters(ctx context.Context) map[string]gopsnet.IOCountersStat {
	stats, _ := gopsnet.IOCountersWithContext(ctx, true)
	result := make(map[string]gopsnet.IOCountersStat, len(stats))
	for _, stat := range stats {
		result[stat.Name] = stat
	}
	return result
}

func collectSimpleNetStats(
	ctx context.Context,
	ifaces []gopsnet.InterfaceStat,
	previousStats,
	currentStats map[string]gopsnet.IOCountersStat,
	intervalSeconds float64,
) []SimpleNetInfo {
	infos := make([]SimpleNetInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		if err := ctx.Err(); err != nil {
			return infos
		}
		if strings.HasPrefix(iface.Name, "lo") {
			continue
		}

		rxKBs, txKBs := computeSimpleNetRates(iface.Name, previousStats, currentStats, intervalSeconds)
		infos = append(infos, SimpleNetInfo{
			Name:  iface.Name,
			IPv4:  collectInterfaceIPv4s(iface),
			MAC:   iface.HardwareAddr,
			Speed: netSpeedReader(ctx, iface.Name),
			TxKBs: txKBs,
			RxKBs: rxKBs,
		})
	}
	return infos
}

func collectInterfaceIPv4s(iface gopsnet.InterfaceStat) []string {
	var ipv4s []string
	for _, addr := range iface.Addrs {
		ip, _, _ := net.ParseCIDR(addr.Addr)
		if ip != nil && ip.To4() != nil {
			ipv4s = append(ipv4s, addr.Addr)
		}
	}
	return ipv4s
}

func readInterfaceSpeed(ctx context.Context, name string) string {
	if err := ctx.Err(); err != nil {
		return "unknown"
	}
	b, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/speed", name))
	if err != nil {
		return "unknown"
	}

	speed := strings.TrimSpace(string(b))
	if speed == "" || speed == "-1" {
		return "unknown"
	}
	return speed + " Mbps"
}

func computeSimpleNetRates(
	name string,
	previousStats,
	currentStats map[string]gopsnet.IOCountersStat,
	intervalSeconds float64,
) (float64, float64) {
	if intervalSeconds <= 0 {
		return 0, 0
	}

	previous, okPrevious := previousStats[name]
	current, okCurrent := currentStats[name]
	if !okPrevious || !okCurrent {
		return 0, 0
	}
	if current.BytesRecv < previous.BytesRecv || current.BytesSent < previous.BytesSent {
		return 0, 0
	}

	rx := float64(current.BytesRecv-previous.BytesRecv) / intervalSeconds / 1024.0
	tx := float64(current.BytesSent-previous.BytesSent) / intervalSeconds / 1024.0
	return rx, tx
}

// Pure fetcher used by the bridge handler map.
func FetchNetworks(ctx context.Context) ([]SimpleNetInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	netRateStateLock.Lock()
	currentStats := netCounterSampler(ctx)
	currentAt := netClock()
	previousStats := lastNetCounters
	previousAt := lastNetSampleAt
	lastNetCounters = currentStats
	lastNetSampleAt = currentAt
	netRateStateLock.Unlock()

	ifaces, _ := netInterfaceReader(ctx)

	intervalSeconds := 0.0
	if !previousAt.IsZero() {
		intervalSeconds = currentAt.Sub(previousAt).Seconds()
	}

	infos := collectSimpleNetStats(ctx, ifaces, previousStats, currentStats, intervalSeconds)
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}
