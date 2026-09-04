package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

type nvtopSnapshot struct {
	DeviceName string         `json:"device_name"`
	Temp       *string        `json:"temp"`
	PowerDraw  *string        `json:"power_draw"`
	GpuUtil    *string        `json:"gpu_util"`
	MemTotal   *string        `json:"mem_total"`
	MemUsed    *string        `json:"mem_used"`
	Processes  []nvtopProcess `json:"processes"`
}

type nvtopProcess struct {
	PID              json.RawMessage `json:"pid"`
	GPUUsage         json.RawMessage `json:"gpu_usage"`
	GPUMemBytesAlloc json.RawMessage `json:"gpu_mem_bytes_alloc"`
	Encode           json.RawMessage `json:"encode"`
	Decode           json.RawMessage `json:"decode"`
	EncodeDecode     json.RawMessage `json:"encode_decode"`
}

type GPUProcessSample struct {
	DeviceID          string
	DeviceName        string
	PID               int32
	GPUUtilization    *float64
	EncodeUtilization *float64
	DecodeUtilization *float64
	MemoryBytes       *uint64
	Source            string
}

func parseNvtopRawNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(s, "%"), "W"))
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil
	}
	var v float64
	if json.Unmarshal(raw, &v) == nil {
		return v, true
	}
	return 0, false
}
func parseNvtopPID(raw json.RawMessage) (int32, bool) {
	v, ok := parseNvtopRawNumber(raw)
	return int32(v), ok && v > 0 && v <= math.MaxInt32 && math.Trunc(v) == v
}

func parseNvtopPercent(raw json.RawMessage) (*float64, bool) {
	v, ok := parseNvtopRawNumber(raw)
	if !ok || v < 0 || v > 100 {
		return nil, false
	}
	return &v, true
}

// parseNvtopNumber parses nvtop numeric strings with units (C/W/%).
func parseNvtopNumber(raw string) float64 {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimSuffix(cleaned, "C")
	cleaned = strings.TrimSuffix(cleaned, "W")
	cleaned = strings.TrimSuffix(cleaned, "%")
	val, _ := strconv.ParseFloat(cleaned, 64)
	return val
}

// updateNvtopSnapshots applies one decoded nvtop snapshot batch to GPU accumulators.
func (gm *GPUManager) updateNvtopSnapshots(snapshots []nvtopSnapshot) bool {
	return gm.applyNvtopSnapshots(snapshots, true)
}

func (gm *GPUManager) updateNvtopProcessSnapshots(snapshots []nvtopSnapshot) bool {
	return gm.applyNvtopSnapshots(snapshots, false)
}

//nolint:gocognit // Reconciliation is a straight-line mapping of optional fields across unstable device ordering.
func (gm *GPUManager) applyNvtopSnapshots(snapshots []nvtopSnapshot, updateAggregates bool) bool {
	gm.Lock()
	defer gm.Unlock()
	if gm.nvtopDeviceNames == nil {
		gm.nvtopDeviceNames = make(map[string]string)
	}
	if gm.processSamples == nil {
		gm.processSamples = make(map[string][]GPUProcessSample)
	}

	valid := false
	processes := make(map[string][]GPUProcessSample, len(snapshots))
	usedIDs := make(map[string]struct{}, len(snapshots))
	for i, sample := range snapshots {
		if sample.DeviceName == "" {
			continue
		}
		indexID := "n" + strconv.Itoa(i)
		id := indexID

		// nvtop ordering can change, so prefer reusing an existing slot with matching device name.
		if existingName := gm.nvtopDeviceNames[indexID]; existingName != "" && existingName != sample.DeviceName {
			for existingID, deviceName := range gm.nvtopDeviceNames {
				if !strings.HasPrefix(existingID, "n") {
					continue
				}
				if _, taken := usedIDs[existingID]; taken {
					continue
				}
				if deviceName == sample.DeviceName {
					id = existingID
					break
				}
			}
		}
		gm.nvtopDeviceNames[id] = sample.DeviceName

		if updateAggregates {
			if _, ok := gm.GpuDataMap[id]; !ok {
				gm.GpuDataMap[id] = &system.GPUData{Name: sample.DeviceName}
			}
			gpu := gm.GpuDataMap[id]
			gpu.Name = sample.DeviceName

			if sample.Temp != nil {
				gpu.Temperature = parseNvtopNumber(*sample.Temp)
			}
			if sample.MemUsed != nil {
				gpu.MemoryUsed = utils.BytesToMegabytes(parseNvtopNumber(*sample.MemUsed))
			}
			if sample.MemTotal != nil {
				gpu.MemoryTotal = utils.BytesToMegabytes(parseNvtopNumber(*sample.MemTotal))
			}
			if sample.GpuUtil != nil {
				gpu.Usage += parseNvtopNumber(*sample.GpuUtil)
			}
			if sample.PowerDraw != nil {
				gpu.Power += parseNvtopNumber(*sample.PowerDraw)
			}
			gpu.Count++
		}
		for _, proc := range sample.Processes {
			pid, ok := parseNvtopPID(proc.PID)
			if !ok {
				continue
			}
			item := GPUProcessSample{DeviceID: id, DeviceName: sample.DeviceName, PID: pid, Source: nvtopCmd}
			if value, known := parseNvtopPercent(proc.GPUUsage); known {
				item.GPUUtilization = value
			}
			if value, known := parseNvtopPercent(proc.Encode); known {
				item.EncodeUtilization = value
			}
			if value, known := parseNvtopPercent(proc.Decode); known {
				item.DecodeUtilization = value
			} else if value, known := parseNvtopPercent(proc.EncodeDecode); known {
				item.DecodeUtilization = value
			}
			if v, known := parseNvtopRawNumber(proc.GPUMemBytesAlloc); known && v >= 0 && v <= math.MaxUint64 {
				bytes := uint64(v)
				item.MemoryBytes = &bytes
			}
			processes[id] = append(processes[id], item)
		}
		usedIDs[id] = struct{}{}
		valid = true
	}
	gm.replaceGPUProcessSourceLocked(nvtopCmd, processes, valid)
	return valid
}

func (gm *GPUManager) GPUProcessSamples() []GPUProcessSample {
	gm.Lock()
	defer gm.Unlock()
	preferred := gm.processSourcePreferred
	preferredReady := preferred != "" && gm.processSourceReady[preferred]
	var out []GPUProcessSample
	for _, items := range gm.processSamples {
		for _, item := range items {
			if !preferredReady || item.Source == preferred {
				out = append(out, item)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeviceID != out[j].DeviceID {
			return out[i].DeviceID < out[j].DeviceID
		}
		if out[i].PID != out[j].PID {
			return out[i].PID < out[j].PID
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func (gm *GPUManager) setGPUProcessSourcePreference(source string) {
	gm.Lock()
	gm.processSourcePreferred = source
	gm.Unlock()
}

func (gm *GPUManager) replaceGPUProcessSourceLocked(source string, replacements map[string][]GPUProcessSample, ready bool) {
	if gm.processSamples == nil {
		gm.processSamples = make(map[string][]GPUProcessSample)
	}
	for key, samples := range gm.processSamples {
		kept := samples[:0]
		for _, sample := range samples {
			if sample.Source != source {
				kept = append(kept, sample)
			}
		}
		if len(kept) == 0 {
			delete(gm.processSamples, key)
		} else {
			gm.processSamples[key] = kept
		}
	}
	for deviceID, samples := range replacements {
		if len(samples) > 0 {
			gm.processSamples[gpuProcessSampleKey(source, deviceID)] = append([]GPUProcessSample(nil), samples...)
		}
	}
	if gm.processSourceReady == nil {
		gm.processSourceReady = make(map[string]bool)
	}
	gm.processSourceReady[source] = ready
}

func (gm *GPUManager) clearGPUProcessSource(source string) {
	gm.Lock()
	gm.replaceGPUProcessSourceLocked(source, nil, false)
	gm.Unlock()
}

func gpuProcessSampleKey(source, deviceID string) string {
	return source + ":" + deviceID
}

// collectNvtopStats runs nvtop loop mode and continuously decodes JSON snapshots.
func (gm *GPUManager) collectNvtopStats(ctx context.Context, interval string, updateAggregates bool) (err error) {
	cmd := exec.CommandContext(ctx, nvtopCmd, "-l", "-d", interval)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if startErr := cmd.Start(); startErr != nil {
		return startErr
	}
	defer cleanupStartedCommand(cmd, stdout, &err)

	decoder := json.NewDecoder(stdout)
	foundValid := false
	for {
		var snapshots []nvtopSnapshot
		if err := decoder.Decode(&snapshots); err != nil {
			if err == io.EOF {
				if foundValid {
					return nil
				}
				return errNoValidData
			}
			return err
		}
		var valid bool
		if updateAggregates {
			valid = gm.updateNvtopSnapshots(snapshots)
		} else {
			valid = gm.updateNvtopProcessSnapshots(snapshots)
		}
		if valid {
			foundValid = true
		}
	}
}

// startNvtopCollector starts nvtop collection with retry or fallback callback handling.
func (gm *GPUManager) startNvtopCollector(ctx context.Context, interval string, onFailure func()) {
	gm.startCollector(ctx, "nvtop", func() {
		runRetryingCollector(ctx, retryWaitTime, func() error {
			return gm.collectNvtopStats(ctx, interval, true)
		}, func(err error) bool {
			gm.clearGPUProcessSource(nvtopCmd)
			slog.Warn("Error collecting GPU data via nvtop", "err", err)
			if onFailure != nil {
				onFailure()
				return false
			}
			return true
		})
	})
}

func (gm *GPUManager) startNvtopProcessCollector(ctx context.Context, interval string) {
	gm.startCollector(ctx, "nvtop-process", func() {
		runRetryingCollector(ctx, retryWaitTime, func() error {
			return gm.collectNvtopStats(ctx, interval, false)
		}, func(err error) bool {
			gm.clearGPUProcessSource(nvtopCmd)
			slog.Warn("Error collecting GPU process data via nvtop", "err", err)
			return true
		})
	})
}
