package app

import (
	"bufio"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"syscall"
	"time"

	psutilNet "github.com/shirou/gopsutil/v4/net"

	modelnet "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/network"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

func collectConnectionStats(ctx context.Context) (*modelnet.ConnectionStats, error) {
	stats := &modelnet.ConnectionStats{Statuses: make(map[string]int)}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conns, err := psutilNet.ConnectionsMaxWithoutUidsWithContext(ctx, "inet", 0)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		slog.Debug("network connection collection failed", "err", err)
	} else {
		stats.NetConnectionsEnabled = true
		for _, conn := range conns {
			stats.Total++
			switch conn.Type {
			case syscall.SOCK_STREAM:
				stats.TCP++
			case syscall.SOCK_DGRAM:
				stats.UDP++
			}
			status := strings.ToUpper(strings.TrimSpace(conn.Status))
			if status == "" {
				continue
			}
			stats.Statuses[status]++
		}
		applyConnectionStatusFields(stats)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	count, max, ok := readConntrackStats()
	if ok {
		stats.NFConntrackEnabled = true
		stats.NFConntrackCount = count
		stats.NFConntrackMax = max
		if max > 0 {
			stats.NFConntrackPercent = utils.TwoDecimals(float64(count) / float64(max) * 100)
		}
	}
	if len(stats.Statuses) == 0 {
		stats.Statuses = nil
	}
	return stats, nil
}

func applyConnectionStatusFields(stats *modelnet.ConnectionStats) {
	stats.Listen = stats.Statuses["LISTEN"]
	stats.Established = stats.Statuses["ESTABLISHED"]
	stats.SynSent = stats.Statuses["SYN_SENT"]
	stats.SynRecv = stats.Statuses["SYN_RECV"]
	stats.FinWait1 = stats.Statuses["FIN_WAIT1"]
	stats.FinWait2 = stats.Statuses["FIN_WAIT2"]
	stats.TimeWait = stats.Statuses["TIME_WAIT"]
	stats.Close = stats.Statuses["CLOSE"]
	stats.CloseWait = stats.Statuses["CLOSE_WAIT"]
	stats.LastAck = stats.Statuses["LAST_ACK"]
	stats.Closing = stats.Statuses["CLOSING"]
}

func readConntrackStats() (count uint64, max uint64, ok bool) {
	count, countOK := utils.ReadUintFile("/proc/sys/net/netfilter/nf_conntrack_count")
	max, maxOK := utils.ReadUintFile("/proc/sys/net/netfilter/nf_conntrack_max")
	return count, max, countOK && maxOK
}

func collectIRQStats(ctx context.Context) ([]modelnet.IRQStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw := utils.ReadStringFile("/proc/interrupts")
	if raw == "" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parseInterrupts(raw), nil
}

func parseInterrupts(raw string) []modelnet.IRQStat {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	items := []modelnet.IRQStat{}
	cpuColumns := 0

	if scanner.Scan() {
		cpuColumns = countInterruptCPUColumns(scanner.Text())
	}

	for scanner.Scan() {
		item, ok := parseInterruptLine(scanner.Text(), cpuColumns)
		if !ok {
			continue
		}
		items = append(items, item)
	}
	return items
}

func countInterruptCPUColumns(header string) int {
	count := 0
	for field := range strings.FieldsSeq(header) {
		if strings.HasPrefix(field, "CPU") {
			count++
		}
	}
	return count
}

func parseInterruptLine(line string, cpuColumns int) (modelnet.IRQStat, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return modelnet.IRQStat{}, false
	}
	irq, rest, ok := strings.Cut(line, ":")
	if !ok {
		return modelnet.IRQStat{}, false
	}

	fields := strings.Fields(rest)
	counts, descriptionStart := parseInterruptCounts(fields, cpuColumns)
	if len(counts) == 0 {
		return modelnet.IRQStat{}, false
	}

	return modelnet.IRQStat{
		IRQ:         strings.TrimSpace(irq),
		Total:       sumUint64(counts),
		CPUCounts:   counts,
		Description: strings.Join(fields[descriptionStart:], " "),
	}, true
}

func parseInterruptCounts(fields []string, cpuColumns int) ([]uint64, int) {
	counts := make([]uint64, 0, cpuColumns)
	descriptionStart := 0
	for i, field := range fields {
		if cpuColumns > 0 && len(counts) >= cpuColumns {
			return counts, i
		}
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return counts, i
		}
		counts = append(counts, value)
		descriptionStart = i + 1
	}
	return counts, descriptionStart
}

func sumUint64(values []uint64) uint64 {
	total := uint64(0)
	for _, value := range values {
		total += value
	}
	return total
}
