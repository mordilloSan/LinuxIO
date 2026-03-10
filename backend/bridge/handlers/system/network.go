// bridge/handlers/system/network.go
package system

import (
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
	simpleNetStats     = make(map[string]SimpleNetInfo)
	simpleNetStatsLock sync.RWMutex
	onceSampler        sync.Once
)

// Call this ONCE from bridge main() (or guard with once).
func StartSimpleNetInfoSampler() {
	onceSampler.Do(func() {
		go runSimpleNetInfoSampler()
	})
}

func runSimpleNetInfoSampler() {
	for {
		stats1Map := sampleIOCounters()
		time.Sleep(1 * time.Second)
		stats2Map := sampleIOCounters()

		simpleNetStatsLock.Lock()
		simpleNetStats = collectSimpleNetStats(stats1Map, stats2Map)
		simpleNetStatsLock.Unlock()
	}
}

func sampleIOCounters() map[string]gopsnet.IOCountersStat {
	stats, _ := gopsnet.IOCounters(true)
	result := make(map[string]gopsnet.IOCountersStat, len(stats))
	for _, stat := range stats {
		result[stat.Name] = stat
	}
	return result
}

func collectSimpleNetStats(stats1Map, stats2Map map[string]gopsnet.IOCountersStat) map[string]SimpleNetInfo {
	ifaces, _ := gopsnet.Interfaces()
	tmp := make(map[string]SimpleNetInfo, len(ifaces))
	for _, iface := range ifaces {
		if strings.HasPrefix(iface.Name, "lo") {
			continue
		}

		rxKBs, txKBs := computeSimpleNetRates(iface.Name, stats1Map, stats2Map)
		tmp[iface.Name] = SimpleNetInfo{
			Name:  iface.Name,
			IPv4:  collectInterfaceIPv4s(iface),
			MAC:   iface.HardwareAddr,
			Speed: readInterfaceSpeed(iface.Name),
			TxKBs: txKBs,
			RxKBs: rxKBs,
		}
	}
	return tmp
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

func readInterfaceSpeed(name string) string {
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

func computeSimpleNetRates(name string, stats1Map, stats2Map map[string]gopsnet.IOCountersStat) (float64, float64) {
	s1, ok1 := stats1Map[name]
	s2, ok2 := stats2Map[name]
	if !ok1 || !ok2 {
		return 0, 0
	}

	rx := max(float64(s2.BytesRecv-s1.BytesRecv)/1024.0, 0)
	tx := max(float64(s2.BytesSent-s1.BytesSent)/1024.0, 0)
	return rx, tx
}

// Pure fetcher used by the bridge handler map.
func FetchNetworks() ([]SimpleNetInfo, error) {
	simpleNetStatsLock.RLock()
	infos := make([]SimpleNetInfo, 0, len(simpleNetStats))
	for _, v := range simpleNetStats {
		infos = append(infos, v)
	}
	simpleNetStatsLock.RUnlock()

	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}
