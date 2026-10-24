package app

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
)

// collectorRun coordinates only the collector pass currently in progress. The
// completed sample is handed to requests that were already waiting and is not
// retained for later requests.
type collectorRun = collector.Run[*collectorAPISample]

type collectorAPISample struct {
	capturedAt int64
	payloads   map[string]jsontext.Value
	summary    jsontext.Value
}

func (a *App) beginCollectorRun() *collectorRun {
	return a.collectorRuns.Begin(struct{}{}, nil)
}

func (a *App) finishCollectorRun(run *collectorRun, sample *collectorAPISample, err error) {
	run.Value = sample
	run.Err = err

	a.collectorRuns.End(struct{}{}, run)
	run.Finish()
}

func (a *App) awaitCollectorSample(ctx context.Context) (*collectorAPISample, bool, error) {
	run := a.collectorRuns.Current(struct{}{})
	if run == nil {
		return nil, false, nil
	}
	return awaitCollectorRun(ctx, run)
}

func awaitCollectorRun(ctx context.Context, run *collectorRun) (*collectorAPISample, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-run.Done():
		if run.Err != nil || run.Value == nil {
			return nil, false, nil
		}
		return run.Value, true, nil
	}
}

func newCollectorAPISample(capturedAt int64, data *systemmodel.CombinedData) (*collectorAPISample, error) {
	if data == nil {
		return nil, fmt.Errorf("collector sample data is nil")
	}

	payloadValues := plugins.Snapshot(data)
	payloads := make(map[string]jsontext.Value, len(payloadValues)+2)
	for plugin, payload := range payloadValues {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal collector %s payload: %w", plugin, err)
		}
		payloads[plugin] = raw
	}

	processes, err := json.Marshal(liveProcessesData{Count: data.ProcessCount, Items: data.Processes})
	if err != nil {
		return nil, fmt.Errorf("marshal collector processes payload: %w", err)
	}
	payloads[plugins.Processes] = processes

	programs, err := json.Marshal(data.Programs)
	if err != nil {
		return nil, fmt.Errorf("marshal collector programs payload: %w", err)
	}
	payloads[plugins.Programs] = programs

	summary, err := json.Marshal(systemmodel.NewSummary(data))
	if err != nil {
		return nil, fmt.Errorf("marshal collector summary payload: %w", err)
	}
	return &collectorAPISample{
		capturedAt: capturedAt,
		payloads:   payloads,
		summary:    summary,
	}, nil
}

func (sample *collectorAPISample) pluginPayload(plugin string) (jsontext.Value, bool) {
	raw, ok := sample.payloads[plugin]
	if !ok {
		return nil, false
	}
	return jsontext.Value(append([]byte(nil), raw...)), true
}

func (sample *collectorAPISample) systemSummary() (systemmodel.Summary, error) {
	var summary systemmodel.Summary
	if err := json.Unmarshal(sample.summary, &summary); err != nil {
		return systemmodel.Summary{}, err
	}
	return summary, nil
}
