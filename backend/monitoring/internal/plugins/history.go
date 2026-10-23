package plugins

import (
	"fmt"
	"strings"
	"time"
)

// ParseHistory parses the history allowlist: "all", "none", or a comma-separated
// list of history-capable plugins. An empty value means unset and yields
// DefaultHistory.
func ParseHistory(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultHistory(), nil
	}

	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "all":
		return HistoryCapable(), nil
	case "none":
		return nil, nil
	}

	seen := make(map[string]struct{})
	var out []string
	for part := range strings.SplitSeq(normalized, ",") {
		plugin := strings.TrimSpace(part)
		if plugin == "" {
			continue
		}
		if !IsName(plugin) {
			return nil, fmt.Errorf("unknown history plugin %q", plugin)
		}
		if IsLiveOnly(plugin) {
			return nil, fmt.Errorf("history is not supported for live-only plugin %q", plugin)
		}
		if _, ok := seen[plugin]; ok {
			continue
		}
		seen[plugin] = struct{}{}
		out = append(out, plugin)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// HistoryEvery converts per-plugin history intervals into a write cadence in
// collector ticks. Every interval must be a whole multiple of the tick; smart
// history follows collector.smart_refresh_interval and live-only plugins have
// no history, so neither accepts an interval.
func HistoryEvery(intervals map[string]time.Duration, tick time.Duration) (map[string]uint64, error) {
	if tick <= 0 {
		return nil, fmt.Errorf("collector interval must be greater than zero")
	}
	out := make(map[string]uint64, len(intervals))
	for plugin, interval := range intervals {
		switch {
		case !IsName(plugin):
			return nil, fmt.Errorf("unknown history plugin %q", plugin)
		case IsLiveOnly(plugin):
			return nil, fmt.Errorf("history is not supported for live-only plugin %q", plugin)
		case plugin == Smart:
			return nil, fmt.Errorf("%q history follows collector.smart_refresh_interval", plugin)
		case interval < tick || interval%tick != 0:
			return nil, fmt.Errorf("%s interval %s must be a whole multiple of the collector interval %s", plugin, interval, tick)
		}
		out[plugin] = uint64(interval / tick)
	}
	return out, nil
}
