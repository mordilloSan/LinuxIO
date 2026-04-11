package monitoring

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPMAPIGetInDomMapRejectsArchiveEnumeration(t *testing.T) {
	_, err := pmapiGetInDomMap(123, true)

	require.Error(t, err)
	require.Contains(t, err.Error(), "pmGetInDomArchive is unsupported")
}

func TestGetCPUSeriesBuildsUsageFromArchiveSamples(t *testing.T) {
	originalQuerySamples := queryPCPSamples
	originalQueryLiveMetric := queryPCPLiveMetric
	t.Cleanup(func() {
		queryPCPSamples = originalQuerySamples
		queryPCPLiveMetric = originalQueryLiveMetric
	})

	queryPCPSamples = func(context.Context, pcpSamplesRequest) ([]SeriesPoint, error) {
		return []SeriesPoint{
			{TS: 0, Value: 0},
			{TS: 5_000, Value: 16_000},
			{TS: 10_000, Value: 24_000},
		}, nil
	}
	queryPCPLiveMetric = func(context.Context, string) (float64, error) {
		return 4, nil
	}

	result := GetCPUSeries(context.Background(), "1m")

	require.True(t, result.Available)
	require.Equal(t, "1m", result.Range)
	require.Equal(t, 5, result.StepSeconds)
	require.Equal(t, []SeriesPoint{
		{TS: 5_000, Value: 20},
		{TS: 10_000, Value: 60},
	}, result.Points)
}

func TestGetMemorySeriesBuildsPercentFromArchiveSamples(t *testing.T) {
	originalQuerySamples := queryPCPSamples
	t.Cleanup(func() {
		queryPCPSamples = originalQuerySamples
	})

	queryPCPSamples = func(_ context.Context, request pcpSamplesRequest) ([]SeriesPoint, error) {
		switch request.Metric {
		case pcpMemoryMetricAvailable:
			return []SeriesPoint{
				{TS: 1_000, Value: 40},
				{TS: 2_000, Value: 20},
			}, nil
		case pcpMemoryMetricPhysical:
			return []SeriesPoint{
				{TS: 1_000, Value: 100},
				{TS: 2_000, Value: 100},
			}, nil
		default:
			t.Fatalf("unexpected metric %q", request.Metric)
			return nil, nil
		}
	}

	result := GetMemorySeries(context.Background(), "1m")

	require.True(t, result.Available)
	require.Equal(t, []SeriesPoint{
		{TS: 1_000, Value: 60},
		{TS: 2_000, Value: 80},
	}, result.Points)
}

func TestGetMemorySeriesReturnsUnavailableOnInvalidRange(t *testing.T) {
	result := GetMemorySeries(context.Background(), "2m")

	require.False(t, result.Available)
	require.Equal(t, "2m", result.Range)
	require.Equal(t, 0, result.StepSeconds)
	require.Equal(t, "unsupported monitoring range", result.Reason)
}

func TestGetNetworkSeriesFiltersInstancesAndBuildsRates(t *testing.T) {
	originalQuerySamples := queryPCPSamples
	originalQueryInstances := queryPCPInstanceNames
	t.Cleanup(func() {
		queryPCPSamples = originalQuerySamples
		queryPCPInstanceNames = originalQueryInstances
	})

	queryPCPInstanceNames = func(context.Context, string) ([]string, error) {
		return []string{"docker0", "eth0", "lo", "eno1"}, nil
	}
	queryPCPSamples = func(_ context.Context, request pcpSamplesRequest) ([]SeriesPoint, error) {
		require.Equal(t, []string{"eno1", "eth0"}, request.Instances)
		switch request.Metric {
		case pcpNetworkMetricReceive:
			return []SeriesPoint{
				{TS: 0, Value: 0},
				{TS: 5_000, Value: 15_360},
				{TS: 10_000, Value: 25_600},
			}, nil
		case pcpNetworkMetricTransmit:
			return []SeriesPoint{
				{TS: 0, Value: 0},
				{TS: 5_000, Value: 5_120},
				{TS: 10_000, Value: 15_360},
			}, nil
		default:
			t.Fatalf("unexpected metric %q", request.Metric)
			return nil, nil
		}
	}

	result := GetNetworkSeries(context.Background(), "1m", "")

	require.True(t, result.Available)
	require.Equal(t, []SeriesPoint{
		{TS: 5_000, Value: 3},
		{TS: 10_000, Value: 2},
	}, result.RXPoints)
	require.Equal(t, []SeriesPoint{
		{TS: 5_000, Value: 1},
		{TS: 10_000, Value: 2},
	}, result.TXPoints)
}

func TestGetDiskIOSeriesFiltersInstancesAndBuildsRates(t *testing.T) {
	originalQuerySamples := queryPCPSamples
	originalQueryInstances := queryPCPInstanceNames
	t.Cleanup(func() {
		queryPCPSamples = originalQuerySamples
		queryPCPInstanceNames = originalQueryInstances
	})

	queryPCPInstanceNames = func(context.Context, string) ([]string, error) {
		return []string{"dm-0", "nvme0n1", "sda"}, nil
	}
	queryPCPSamples = func(_ context.Context, request pcpSamplesRequest) ([]SeriesPoint, error) {
		require.Equal(t, []string{"nvme0n1", "sda"}, request.Instances)
		switch request.Metric {
		case pcpDiskMetricRead:
			return []SeriesPoint{
				{TS: 0, Value: 0},
				{TS: 5_000, Value: 10},
				{TS: 10_000, Value: 20},
			}, nil
		case pcpDiskMetricWrite:
			return []SeriesPoint{
				{TS: 0, Value: 0},
				{TS: 5_000, Value: 5},
				{TS: 10_000, Value: 15},
			}, nil
		default:
			t.Fatalf("unexpected metric %q", request.Metric)
			return nil, nil
		}
	}

	result := GetDiskIOSeries(context.Background(), "1m", "")

	require.True(t, result.Available)
	require.Equal(t, []SeriesPoint{
		{TS: 5_000, Value: 2_048},
		{TS: 10_000, Value: 2_048},
	}, result.ReadPoints)
	require.Equal(t, []SeriesPoint{
		{TS: 5_000, Value: 1_024},
		{TS: 10_000, Value: 2_048},
	}, result.WritePoints)
}

func TestGetGPUSeriesReturnsUnavailableUntilPCPGPUMetricsAreDefined(t *testing.T) {
	result := GetGPUSeries(context.Background(), "1m")

	require.False(t, result.Available)
	require.Equal(t, "1m", result.Range)
	require.Equal(t, 5, result.StepSeconds)
	require.Equal(t, pcpGPUReason, result.Reason)
}

func TestGetCPUSeriesReturnsFriendlyTimeoutReason(t *testing.T) {
	originalQuerySamples := queryPCPSamples
	t.Cleanup(func() {
		queryPCPSamples = originalQuerySamples
	})

	queryPCPSamples = func(context.Context, pcpSamplesRequest) ([]SeriesPoint, error) {
		return nil, context.DeadlineExceeded
	}

	result := GetCPUSeries(context.Background(), "1m")

	require.False(t, result.Available)
	require.Equal(t, pcpHistoryTimeoutReason, result.Reason)
}

func TestNormalizePCPQueryErrorMapsLibpcpErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: pcpHistoryTimeoutReason,
		},
		{
			name:     "libpcp not found",
			err:      fmt.Errorf("cannot load libpcp: dlopen failed"),
			expected: pcpHistoryLibReason,
		},
		{
			name:     "archive not found",
			err:      fmt.Errorf("failed to find PCP archive path: no such file"),
			expected: pcpHistorySetupReason,
		},
		{
			name:     "pmapi error",
			err:      fmt.Errorf("pmNewContext: PMAPI error -12350"),
			expected: pcpHistorySetupReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePCPQueryError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}
