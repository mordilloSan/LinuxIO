package system

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/session"
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
)

// Call this ONCE in main()
func StartSimpleNetInfoSampler() {
	go func() {
		for {
			// Take first snapshot (t0)
			stat1, _ := gopsnet.IOCounters(true)
			stats1Map := make(map[string]gopsnet.IOCountersStat)
			for _, s := range stat1 {
				stats1Map[s.Name] = s
			}
			time.Sleep(1 * time.Second)

			// Take second snapshot (t1)
			stat2, _ := gopsnet.IOCounters(true)
			stats2Map := make(map[string]gopsnet.IOCountersStat)
			for _, s := range stat2 {
				stats2Map[s.Name] = s
			}

			ifaces, _ := gopsnet.Interfaces()
			tmp := make(map[string]SimpleNetInfo)

			for _, iface := range ifaces {
				if strings.HasPrefix(iface.Name, "lo") {
					continue
				}
				var ipv4s []string
				for _, addr := range iface.Addrs {
					ip, _, _ := net.ParseCIDR(addr.Addr)
					if ip != nil && ip.To4() != nil {
						ipv4s = append(ipv4s, addr.Addr)
					}
				}
				mac := iface.HardwareAddr
				speed := "unknown"
				if b, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/speed", iface.Name)); err == nil {
					s := strings.TrimSpace(string(b))
					if s != "" && s != "-1" {
						speed = s + " Mbps"
					}
				}
				rx_speed, tx_speed := 0.0, 0.0
				s1, ok1 := stats1Map[iface.Name]
				s2, ok2 := stats2Map[iface.Name]
				if ok1 && ok2 {
					rx_speed = float64(s2.BytesRecv-s1.BytesRecv) / 1024.0 // KB/s
					tx_speed = float64(s2.BytesSent-s1.BytesSent) / 1024.0 // KB/s
					if rx_speed < 0 {
						rx_speed = 0
					}
					if tx_speed < 0 {
						tx_speed = 0
					}
				}
				tmp[iface.Name] = SimpleNetInfo{
					Name:  iface.Name,
					IPv4:  ipv4s,
					MAC:   mac,
					Speed: speed,
					TxKBs: tx_speed,
					RxKBs: rx_speed,
				}
			}

			simpleNetStatsLock.Lock()
			simpleNetStats = tmp
			simpleNetStatsLock.Unlock()
			// loop every second
		}
	}()
}

func getNetworks(c *gin.Context) {
	sess := session.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	simpleNetStatsLock.RLock()
	infos := make([]SimpleNetInfo, 0, len(simpleNetStats))
	for _, v := range simpleNetStats {
		infos = append(infos, v)
	}
	simpleNetStatsLock.RUnlock()

	// Sort by Name for consistent API output
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	c.JSON(http.StatusOK, infos)
}
