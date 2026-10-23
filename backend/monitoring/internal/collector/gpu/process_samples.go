package gpu

type processIdentity struct {
	deviceID string
	pid      int32
}

// UniqueProcessSamples keeps one sample per device and process, preferring the
// richer source and filling its missing metrics from the other.
func UniqueProcessSamples(samples []ProcessSample) []ProcessSample {
	unique := make(map[processIdentity]ProcessSample, len(samples))
	for _, sample := range samples {
		key := processIdentity{deviceID: sample.DeviceID, pid: sample.PID}
		if key.deviceID == "" {
			key.deviceID = sample.DeviceName
		}
		if key.deviceID == "" || key.pid <= 0 {
			continue
		}
		current, exists := unique[key]
		if !exists {
			unique[key] = sample
			continue
		}
		if processSourceRank(sample.Source) > processSourceRank(current.Source) {
			sample = fillMissingProcessMetrics(sample, current)
			unique[key] = sample
		} else {
			unique[key] = fillMissingProcessMetrics(current, sample)
		}
	}
	result := make([]ProcessSample, 0, len(unique))
	for _, sample := range unique {
		result = append(result, sample)
	}
	return result
}

func fillMissingProcessMetrics(preferred, fallback ProcessSample) ProcessSample {
	usedFallback := false
	if preferred.DeviceName == "" {
		preferred.DeviceName = fallback.DeviceName
		usedFallback = fallback.DeviceName != ""
	}
	if preferred.GPUUtilization == nil {
		preferred.GPUUtilization = fallback.GPUUtilization
		usedFallback = usedFallback || fallback.GPUUtilization != nil
	}
	if preferred.EncodeUtilization == nil {
		preferred.EncodeUtilization = fallback.EncodeUtilization
		usedFallback = usedFallback || fallback.EncodeUtilization != nil
	}
	if preferred.DecodeUtilization == nil {
		preferred.DecodeUtilization = fallback.DecodeUtilization
		usedFallback = usedFallback || fallback.DecodeUtilization != nil
	}
	if preferred.MemoryBytes == nil {
		preferred.MemoryBytes = fallback.MemoryBytes
		usedFallback = usedFallback || fallback.MemoryBytes != nil
	}
	if usedFallback {
		preferred.Source = MergeProcessSource(preferred.Source, fallback.Source)
	}
	return preferred
}

func processSourceRank(source string) int {
	switch source {
	case "mixed":
		return 3
	case nvtopCmd:
		return 2
	case string(collectorSourceNVML):
		return 1
	default:
		return 0
	}
}

// MergeProcessSource combines two sample sources, reporting "mixed" when they differ.
func MergeProcessSource(current, next string) string {
	if current == "" {
		return next
	}
	if next == "" || current == next {
		return current
	}
	return "mixed"
}
