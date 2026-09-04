package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

// collectorRun coordinates only the collector pass currently in progress. The
// completed sample is handed to requests that were already waiting and is not
// retained for later requests.
type collectorRun struct {
	done   chan struct{}
	sample *collectorAPISample
	err    error
}

type collectorAPISample struct {
	capturedAt int64
	payloads   map[string]json.RawMessage
	summary    json.RawMessage
}

func (a *App) beginCollectorRun() *collectorRun {
	run := &collectorRun{done: make(chan struct{})}
	a.collectorHandoffMu.Lock()
	a.activeCollectorRun = run
	a.collectorHandoffMu.Unlock()
	return run
}

func (a *App) finishCollectorRun(run *collectorRun, sample *collectorAPISample, err error) {
	run.sample = sample
	run.err = err

	a.collectorHandoffMu.Lock()
	if a.activeCollectorRun == run {
		a.activeCollectorRun = nil
	}
	a.collectorHandoffMu.Unlock()
	close(run.done)
}

func (a *App) awaitCollectorSample(ctx context.Context) (*collectorAPISample, bool, error) {
	a.collectorHandoffMu.Lock()
	run := a.activeCollectorRun
	a.collectorHandoffMu.Unlock()
	if run == nil {
		return nil, false, nil
	}
	return awaitCollectorRun(ctx, run)
}

func awaitCollectorRun(ctx context.Context, run *collectorRun) (*collectorAPISample, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-run.done:
		if run.err != nil || run.sample == nil {
			return nil, false, nil
		}
		return run.sample, true, nil
	}
}

func newCollectorAPISample(capturedAt int64, data *system.CombinedData) (*collectorAPISample, error) {
	if data == nil {
		return nil, fmt.Errorf("collector sample data is nil")
	}

	payloadValues := store.SnapshotPluginPayloads(data)
	payloads := make(map[string]json.RawMessage, len(payloadValues)+2)
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
	payloads[store.PluginProcesses] = processes

	programs, err := json.Marshal(data.Programs)
	if err != nil {
		return nil, fmt.Errorf("marshal collector programs payload: %w", err)
	}
	payloads[store.PluginPrograms] = programs

	summary, err := json.Marshal(system.NewSummary(data))
	if err != nil {
		return nil, fmt.Errorf("marshal collector summary payload: %w", err)
	}
	return &collectorAPISample{
		capturedAt: capturedAt,
		payloads:   payloads,
		summary:    summary,
	}, nil
}

func (sample *collectorAPISample) pluginPayload(plugin string) (json.RawMessage, bool) {
	raw, ok := sample.payloads[plugin]
	if !ok {
		return nil, false
	}
	return json.RawMessage(append([]byte(nil), raw...)), true
}

func (sample *collectorAPISample) systemSummary() (system.Summary, error) {
	var summary system.Summary
	if err := json.Unmarshal(sample.summary, &summary); err != nil {
		return system.Summary{}, err
	}
	return summary, nil
}
