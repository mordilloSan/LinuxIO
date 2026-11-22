package capabilities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// probeTimeout is how long the scan waits for one capability's answer. It is
// above the 5 seconds the slowest probes give themselves (the login shell,
// the monitoring daemon), so their own message is the one reported.
const probeTimeout = 6 * time.Second

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
			// A daemon that takes the connection and never answers, as
			// libvirtd does while it stops, is reported as unavailable; it
			// does not hold back the answers of the others.
			probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			ok, errMsg := spec.DetectFor(probeCtx, user)
			if !ok && errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
				errMsg = "did not answer within " + probeTimeout.String()
			}
			results[index].ok, results[index].errMsg = ok, errMsg
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
