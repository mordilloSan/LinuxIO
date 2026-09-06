package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type liveProcessRun struct {
	done       chan struct{}
	capturedAt time.Time
	payloads   map[string]json.RawMessage
	err        error
}

func (a *App) currentLiveProcessPayloads(ctx context.Context) (map[string]json.RawMessage, time.Time, error) {
	for {
		a.liveProcessMu.Lock()
		run := a.liveProcessRun
		if run == nil || (!isLiveProcessRunActive(run) && (run.err != nil || time.Since(run.capturedAt) >= liveReuseWindow)) {
			run = &liveProcessRun{done: make(chan struct{})}
			a.liveProcessRun = run
			a.liveProcessMu.Unlock()
			a.runLiveProcessCollection(ctx, run)
			if run.err == nil {
				return cloneJSONPayloads(run.payloads), run.capturedAt, nil
			}
			return nil, time.Time{}, run.err
		}
		a.liveProcessMu.Unlock()

		select {
		case <-ctx.Done():
			return nil, time.Time{}, ctx.Err()
		case <-run.done:
		}
		if run.err == nil {
			return cloneJSONPayloads(run.payloads), run.capturedAt, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, time.Time{}, ctxErr
		}
	}
}

func isLiveProcessRunActive(run *liveProcessRun) bool {
	select {
	case <-run.done:
		return false
	default:
		return true
	}
}

func (a *App) runLiveProcessCollection(ctx context.Context, run *liveProcessRun) {
	defer func() {
		if run.err == nil && run.payloads == nil {
			run.err = errors.New("live process collection did not complete")
		}
		run.capturedAt = time.Now()
		close(run.done)
	}()
	if a.liveProcessCollect != nil {
		run.payloads, run.err = a.liveProcessCollect(ctx)
		return
	}
	identities, err := a.collectLiveContainerIdentities(ctx)
	if err != nil {
		run.err = err
		return
	}
	run.payloads, run.err = a.collectProcessPluginPayloadsWithIdentities(ctx, identities)
}

func cloneJSONPayloads(payloads map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(payloads))
	for key, payload := range payloads {
		cloned[key] = append(json.RawMessage(nil), payload...)
	}
	return cloned
}
