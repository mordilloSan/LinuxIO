//go:build linux

package monitoring

import (
	"context"
	"fmt"

	internalpcp "github.com/mordilloSan/LinuxIO/backend/internal/pcp"
)

func pmapiGetInDomMap(indom uint32, archive bool) (map[string]int32, error) {
	if archive {
		return nil, fmt.Errorf("pmGetInDomArchive is unsupported with purego; use pmLookupInDomArchive instead")
	}
	return internalpcp.GetInDomMap(indom)
}

func pmapiQuerySamples(ctx context.Context, req pcpSamplesRequest) ([]SeriesPoint, error) {
	archivePath, err := getCachedArchivePath()
	if err != nil {
		return nil, err
	}

	points, err := internalpcp.QueryArchiveSamples(ctx, archivePath, internalpcp.ArchiveQuery{
		Metric:     req.Metric,
		Instances:  req.Instances,
		Range:      internalpcp.ArchiveRange{Duration: req.Range.Duration, Step: req.Range.Step},
		ExtraCount: req.ExtraCount,
		NoInterp:   req.NoInterp,
	})
	if err != nil {
		return nil, err
	}

	result := make([]SeriesPoint, 0, len(points))
	for _, point := range points {
		result = append(result, SeriesPoint{
			TS:    point.TS,
			Value: point.Value,
		})
	}
	return result, nil
}

func pmapiQueryLiveMetric(ctx context.Context, metric string) (float64, error) {
	return internalpcp.QueryLiveMetric(ctx, metric)
}

func pmapiQueryInstanceNames(ctx context.Context, metric string) ([]string, error) {
	return internalpcp.QueryInstanceNames(ctx, metric)
}
