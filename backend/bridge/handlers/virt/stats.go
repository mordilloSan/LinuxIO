package virt

import (
	"log/slog"
	"strconv"
	"sync"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// domainCounters is one libvirt stats sample; the cumulative values only
// become rates against an earlier sample.
type domainCounters struct {
	at                 time.Time
	cpuNS              uint64
	rx, tx, read, wrte uint64
}

var (
	statsNow = time.Now
	// lastStats keeps the previous sample per domain UUID for the life of
	// this bridge process, so rates are available from the second poll on.
	// shortcut: entries for deleted VMs are never pruned, upgrade if VM churn is high
	lastStats   = map[string]domainCounters{}
	lastStatsMu sync.Mutex
)

// enrichMetrics fills Metrics on the running VMs in vms from one bulk libvirt
// stats call. Stats are best effort: a failure leaves Metrics nil.
func enrichMetrics(conn libvirtConn, domains []libvirt.Domain, vms []apischema.VirtualMachine) {
	if len(domains) == 0 {
		return
	}
	stats := uint32(libvirt.DomainStatsCPUTotal | libvirt.DomainStatsBalloon | libvirt.DomainStatsInterface | libvirt.DomainStatsBlock)
	records, err := conn.ConnectGetAllDomainStats(domains, stats, uint32(libvirt.ConnectGetAllDomainsStatsNowait))
	if err != nil {
		slog.Debug("libvirt domain stats unavailable", "error", err)
		return
	}
	byName := make(map[string]libvirt.DomainStatsRecord, len(records))
	for _, record := range records {
		byName[record.Dom.Name] = record
	}
	now := statsNow()
	for idx := range vms {
		record, ok := byName[vms[idx].Name]
		if !ok {
			continue
		}
		params := newTypedParams(record.Params)
		cpuNS, ok := params.uint("cpu.time")
		if !ok {
			continue // only running domains report CPU time
		}
		current := domainCounters{at: now, cpuNS: cpuNS}
		for i := range int(params.count("net.count")) {
			prefix := "net." + strconv.Itoa(i)
			current.rx += params.value(prefix + ".rx.bytes")
			current.tx += params.value(prefix + ".tx.bytes")
		}
		for i := range int(params.count("block.count")) {
			prefix := "block." + strconv.Itoa(i)
			current.read += params.value(prefix + ".rd.bytes")
			current.wrte += params.value(prefix + ".wr.bytes")
		}
		key := vms[idx].UUID
		if key == "" {
			key = vms[idx].Name
		}
		lastStatsMu.Lock()
		previous, hasPrevious := lastStats[key]
		lastStats[key] = current
		lastStatsMu.Unlock()
		metrics := &apischema.VMMetrics{MemoryUsedMB: int(params.value("balloon.rss") / 1024)}
		if hasPrevious {
			metrics = ratesSince(previous, current, max(vms[idx].VCPUs, 1), metrics)
		}
		vms[idx].Metrics = metrics
	}
}

func ratesSince(previous, current domainCounters, vcpus int, metrics *apischema.VMMetrics) *apischema.VMMetrics {
	elapsed := current.at.Sub(previous.at).Seconds()
	if elapsed <= 0 || current.cpuNS < previous.cpuNS {
		return metrics // clock went backwards or the domain restarted
	}
	percent := min(float64(current.cpuNS-previous.cpuNS)/(elapsed*1e9*float64(vcpus))*100, 100)
	metrics.CPUPercent = &percent
	metrics.NetworkReceiveBytesPerSecond = rate(previous.rx, current.rx, elapsed)
	metrics.NetworkSendBytesPerSecond = rate(previous.tx, current.tx, elapsed)
	metrics.DiskReadBytesPerSecond = rate(previous.read, current.read, elapsed)
	metrics.DiskWriteBytesPerSecond = rate(previous.wrte, current.wrte, elapsed)
	return metrics
}

func rate(previous, current uint64, elapsed float64) *float64 {
	if current < previous {
		return nil
	}
	value := float64(current-previous) / elapsed
	return &value
}

type typedParams map[string]libvirt.TypedParamValue

func (p typedParams) uint(key string) (uint64, bool) {
	switch v := p[key].I.(type) {
	case uint64:
		return v, true
	case int64:
		return uint64(max(v, 0)), true
	case uint32:
		return uint64(v), true
	case int32:
		return uint64(max(v, 0)), true
	default:
		return 0, false
	}
}

func (p typedParams) value(key string) uint64 {
	v, _ := p.uint(key)
	return v
}

func (p typedParams) count(key string) uint64 {
	return min(p.value(key), 64) // bound the loop against a bogus count
}

func newTypedParams(params []libvirt.TypedParam) typedParams {
	out := make(typedParams, len(params))
	for _, param := range params {
		out[param.Field] = param.Value
	}
	return out
}
