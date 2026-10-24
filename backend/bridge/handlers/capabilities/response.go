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

// setCapabilityField writes (ok, errMsg) into the matching fields of out for
// the given wire name. The available/error fields are promoted from the shared
// session.Capabilities* structs embedded in the wire response; the bare switch
// keeps them strongly typed, and the anti-drift test guarantees every wire name
// has a matching field, so no silent misses are possible.
func setCapabilityField(out *apischema.CapabilitiesResponse, name string, ok bool, errMsg string) {
	var errPtr *string
	if errMsg != "" {
		msg := errMsg
		errPtr = &msg
	}
	switch name {
	case "docker":
		out.DockerAvailable, out.DockerError = ok, errPtr
	case "docker_updates":
		out.DockerUpdatesAvailable, out.DockerUpdatesError = ok, errPtr
	case "monitoring":
		out.MonitoringAvailable, out.MonitoringError = ok, errPtr
	case "lm_sensors":
		out.LMSensorsAvailable, out.LMSensorsError = ok, errPtr
	case "memory_inventory":
		out.MemoryInventoryAvailable, out.MemoryInventoryError = ok, errPtr
	case "smartmontools":
		out.SmartmontoolsAvailable, out.SmartmontoolsError = ok, errPtr
	case "packagekit":
		out.PackageKitAvailable, out.PackageKitError = ok, errPtr
	case "nfs_client":
		out.NFSClientAvailable, out.NFSClientError = ok, errPtr
	case "nfs_server":
		out.NFSServerAvailable, out.NFSServerError = ok, errPtr
	case "samba_server":
		out.SambaServerAvailable, out.SambaServerError = ok, errPtr
	case "samba_client":
		out.SambaClientAvailable, out.SambaClientError = ok, errPtr
	case "rsync":
		out.RsyncAvailable, out.RsyncError = ok, errPtr
	case "node":
		out.NodeAvailable, out.NodeError = ok, errPtr
	case "tuned":
		out.TunedAvailable, out.TunedError = ok, errPtr
	case "avahi":
		out.AvahiAvailable, out.AvahiError = ok, errPtr
	case "wireguard":
		out.WireGuardAvailable, out.WireGuardError = ok, errPtr
	case "libvirt":
		out.LibvirtAvailable, out.LibvirtError = ok, errPtr
	case "btrfs_maintenance":
		out.BtrfsMaintenanceAvailable, out.BtrfsMaintenanceError = ok, errPtr
	case "btrfs":
		out.BtrfsAvailable, out.BtrfsError = ok, errPtr
	default:
		panic("capabilities: unknown capability wire name " + name)
	}
}

func buildCapabilitiesResponse(ctx context.Context, user session.User) (apischema.CapabilitiesResponse, error) {
	slog.Debug("checking system capabilities")

	if err := ctx.Err(); err != nil {
		return apischema.CapabilitiesResponse{}, err
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
		return apischema.CapabilitiesResponse{}, err
	}

	var out apischema.CapabilitiesResponse
	summary := make([]string, 0, len(capabilityRegistry))

	for index, spec := range capabilityRegistry {
		result := results[index]
		setCapabilityField(&out, spec.Name, result.ok, result.errMsg)
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
