package capabilities

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func capabilityStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}

func logUnavailableCapability(name, message string) {
	if message == "" {
		return
	}
	slog.Debug("capability unavailable", "capability", name, "error", message)
}

func buildCapabilitiesResponse(ctx context.Context, user session.User) (apischema.CapabilitiesResponse, error) {
	slog.Debug("checking system capabilities")

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	type detectionResult struct {
		ok       bool
		errMsg   string
		duration time.Duration
	}
	results := make([]detectionResult, len(capabilityRegistry))
	detectionStarted := time.Now()
	var wg sync.WaitGroup
	for index, spec := range capabilityRegistry {
		wg.Go(func() {
			started := time.Now()
			results[index].ok, results[index].errMsg = spec.DetectFor(ctx, user)
			results[index].duration = time.Since(started)
		})
	}
	wg.Wait()
	detectionDuration := time.Since(detectionStarted)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := make(apischema.CapabilitiesResponse, len(capabilityRegistry))
	summary := make([]string, 0, len(capabilityRegistry))

	for index, spec := range capabilityRegistry {
		result := results[index]
		status := apischema.CapabilityStatus{Available: result.ok}
		if result.errMsg != "" {
			status.Error = new(result.errMsg)
		}
		out[apischema.CapabilityName(spec.Name)] = status
		summary = append(summary, fmt.Sprintf("%s=%s", strings.ReplaceAll(spec.Name, "_", "-"), capabilityStatus(result.ok)))
		logUnavailableCapability(spec.LogName, result.errMsg)
	}

	slog.Debug("capabilities summary", "summary", strings.Join(summary, " "))
	timingAttrs := make([]slog.Attr, 0, len(capabilityRegistry)+1)
	timingAttrs = append(timingAttrs, slog.Int64("capabilities_us", detectionDuration.Microseconds()))
	for index, spec := range capabilityRegistry {
		timingAttrs = append(timingAttrs,
			slog.Int64("capabilities_"+spec.Name+"_us", results[index].duration.Microseconds()))
	}
	slog.LogAttrs(ctx, slog.LevelDebug, "capabilities timing", timingAttrs...)

	return out, nil
}
