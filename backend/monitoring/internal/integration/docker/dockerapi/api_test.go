package dockerapi

import "testing"

func TestStatsCalculateCPUPercentLinux(t *testing.T) {
	tests := []struct {
		name             string
		stats            Stats
		prevCpuContainer uint64
		prevCpuSystem    uint64
		want             float64
	}{
		{
			name: "calculates percentage from container and system deltas",
			stats: Stats{CPUStats: CPUStats{
				CPUUsage:    CPUUsage{TotalUsage: 1_500},
				SystemUsage: 12_000,
			}},
			prevCpuContainer: 1_000,
			prevCpuSystem:    10_000,
			want:             25,
		},
		{
			name: "first sample establishes baseline without reporting usage",
			stats: Stats{CPUStats: CPUStats{
				CPUUsage:    CPUUsage{TotalUsage: 1_500},
				SystemUsage: 12_000,
			}},
			prevCpuContainer: 0,
			prevCpuSystem:    10_000,
			want:             0,
		},
		{
			name: "unchanged system counter cannot produce a rate",
			stats: Stats{CPUStats: CPUStats{
				CPUUsage:    CPUUsage{TotalUsage: 1_500},
				SystemUsage: 10_000,
			}},
			prevCpuContainer: 1_000,
			prevCpuSystem:    10_000,
			want:             0,
		},
		{
			name: "container counter reset is ignored",
			stats: Stats{CPUStats: CPUStats{
				CPUUsage:    CPUUsage{TotalUsage: 500},
				SystemUsage: 12_000,
			}},
			prevCpuContainer: 1_000,
			prevCpuSystem:    10_000,
			want:             0,
		},
		{
			name: "system counter reset is ignored",
			stats: Stats{CPUStats: CPUStats{
				CPUUsage:    CPUUsage{TotalUsage: 1_500},
				SystemUsage: 9_000,
			}},
			prevCpuContainer: 1_000,
			prevCpuSystem:    10_000,
			want:             0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stats.CalculateCPUPercentLinux(tt.prevCpuContainer, tt.prevCpuSystem); got != tt.want {
				t.Fatalf("CalculateCPUPercentLinux() = %v, want %v", got, tt.want)
			}
		})
	}
}
