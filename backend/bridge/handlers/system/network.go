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
		go func() {
			for {
				// Snapshot t0
				stat1, _ := gopsnet.IOCounters(true)
				stats1Map := make(map[string]gopsnet.IOCountersStat, len(stat1))
				for _, s := range stat1 {
					stats1Map[s.Name] = s
				}
				time.Sleep(1 * time.Second)

				// Snapshot t1
				stat2, _ := gopsnet.IOCounters(true)
				stats2Map := make(map[string]gopsnet.IOCountersStat, len(stat2))
				for _, s := range stat2 {
					stats2Map[s.Name] = s
				}

				ifaces, _ := gopsnet.Interfaces()
				tmp := make(map[string]SimpleNetInfo, len(ifaces))

				for _, iface := range ifaces {
					if strings.HasPrefix(iface.Name, "lo") {
						continue
					}

					// collect IPv4s (addr.Addr is CIDR already)
					var ipv4s []string
					for _, addr := range iface.Addrs {
						ip, _, _ := net.ParseCIDR(addr.Addr)
						if ip != nil && ip.To4() != nil {
							ipv4s = append(ipv4s, addr.Addr)
						}
					}

					// link speed
					speed := "unknown"
					if b, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/speed", iface.Name)); err == nil {
						if s := strings.TrimSpace(string(b)); s != "" && s != "-1" {
							speed = s + " Mbps"
						}
					}

					// rx/tx KB/s
					var rxKBs, txKBs float64
					if s1, ok1 := stats1Map[iface.Name]; ok1 {
						if s2, ok2 := stats2Map[iface.Name]; ok2 {
							rx := float64(s2.BytesRecv-s1.BytesRecv) / 1024.0
							tx := float64(s2.BytesSent-s1.BytesSent) / 1024.0
							if rx < 0 {
								rx = 0
							}
							if tx < 0 {
								tx = 0
							}
							rxKBs, txKBs = rx, tx
						}
					}

					tmp[iface.Name] = SimpleNetInfo{
						Name:  iface.Name,
						IPv4:  ipv4s,
						MAC:   iface.HardwareAddr,
						Speed: speed,
						TxKBs: txKBs,
						RxKBs: rxKBs,
					}
				}

				simpleNetStatsLock.Lock()
				simpleNetStats = tmp
				simpleNetStatsLock.Unlock()
				// loop continues; 1s spacing already provided above
			}
		}()
	})
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
