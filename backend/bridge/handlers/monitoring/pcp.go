package monitoring

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultRangeKey  = "1m"
	pcpArchiveEnv    = "LINUXIO_PCP_ARCHIVE"
	pcpConfigPath    = "/etc/pcp.conf"
	defaultPCPLogDir = "/var/log/pcp"

	pcpHistoryUnavailableReason = "no PCP historical samples available yet"
	pcpHistorySetupReason       = "PCP history is not ready. Check that pmcd and pmlogger services are running."
	pcpHistoryTimeoutReason     = "PCP history query timed out. Check that pmcd and pmlogger services are running."
	pcpHistoryLibReason         = "libpcp is not installed. Install the pcp package."
	pcpGPUReason                = "GPU history is not available"
)

const (
	pcpCPUMetricIdle         = "kernel.all.cpu.idle"
	pcpCPUCountMetric        = "hinv.ncpu"
	pcpMemoryMetricPhysical  = "mem.physmem"
	pcpMemoryMetricAvailable = "mem.util.available"
	pcpNetworkMetricReceive  = "network.interface.in.bytes"
	pcpNetworkMetricTransmit = "network.interface.out.bytes"
	pcpDiskMetricRead        = "disk.dev.read_bytes"
	pcpDiskMetricWrite       = "disk.dev.write_bytes"

	pcpNetworkExcludePattern = `^(lo|veth.*|docker.*|br.*)$`
	pcpDiskDevicePattern     = `^(sd[a-z]+|hd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme[0-9]+n[0-9]+|mmcblk[0-9]+)$`
)

type rangeDefinition struct {
	Key      string
	Duration time.Duration
	Step     time.Duration
}

type SeriesPoint struct {
	TS    int64   `json:"ts"`
	Value float64 `json:"value"`
}

type SeriesResponse struct {
	Available   bool          `json:"available"`
	Range       string        `json:"range"`
	StepSeconds int           `json:"stepSeconds"`
	Points      []SeriesPoint `json:"points"`
	Reason      string        `json:"reason,omitempty"`
}

type NetworkSeriesResponse struct {
	Available   bool          `json:"available"`
	Range       string        `json:"range"`
	StepSeconds int           `json:"stepSeconds"`
	RXPoints    []SeriesPoint `json:"rxPoints"`
	TXPoints    []SeriesPoint `json:"txPoints"`
	Reason      string        `json:"reason,omitempty"`
}

type DiskIOSeriesResponse struct {
	Available   bool          `json:"available"`
	Range       string        `json:"range"`
	StepSeconds int           `json:"stepSeconds"`
	ReadPoints  []SeriesPoint `json:"readPoints"`
	WritePoints []SeriesPoint `json:"writePoints"`
	Reason      string        `json:"reason,omitempty"`
}

type pcpSamplesRequest struct {
	Metric     string
	Instances  []string
	Range      rangeDefinition
	ExtraCount int
	NoInterp   bool
}

var (
	rangeDefinitions = map[string]rangeDefinition{
		"1m":  {Key: "1m", Duration: time.Minute, Step: 5 * time.Second},
		"5m":  {Key: "5m", Duration: 5 * time.Minute, Step: 5 * time.Second},
		"15m": {Key: "15m", Duration: 15 * time.Minute, Step: 15 * time.Second},
		"60m": {Key: "60m", Duration: time.Hour, Step: time.Minute},
		"6h":  {Key: "6h", Duration: 6 * time.Hour, Step: 5 * time.Minute},
		"24h": {Key: "24h", Duration: 24 * time.Hour, Step: 15 * time.Minute},
		"7d":  {Key: "7d", Duration: 7 * 24 * time.Hour, Step: time.Hour},
		"30d": {Key: "30d", Duration: 30 * 24 * time.Hour, Step: 6 * time.Hour},
	}
	pcpNetworkExcludeRE = regexp.MustCompile(pcpNetworkExcludePattern)
	pcpDiskDeviceRE     = regexp.MustCompile(pcpDiskDevicePattern)

	resolvePCPArchivePath = detectPCPArchivePath
	queryPCPLiveMetric    = pmapiQueryLiveMetric
	queryPCPInstanceNames = pmapiQueryInstanceNames
	queryPCPSamples       = pmapiQuerySamples

	cachedArchivePath   string
	cachedArchivePathMu sync.Mutex
	cachedArchivePathAt time.Time
	archiveCacheTTL     = 30 * time.Second
)

func GetCPUSeries(ctx context.Context, rangeKey string) SeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableSeries(rangeKey, 0, "unsupported monitoring range")
	}
	step := int(def.Step / time.Second)

	type samplesResult struct {
		pts []SeriesPoint
		err error
	}
	type countResult struct {
		val float64
		err error
	}

	samplesCh := make(chan samplesResult, 1)
	countCh := make(chan countResult, 1)

	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric:     pcpCPUMetricIdle,
			Range:      def,
			ExtraCount: 1,
		})
		samplesCh <- samplesResult{pts, err}
	}()
	go func() {
		val, err := queryPCPLiveMetric(ctx, pcpCPUCountMetric)
		countCh <- countResult{val, err}
	}()

	samples := <-samplesCh
	count := <-countCh

	if samples.err != nil {
		return unavailableSeries(def.Key, step, normalizePCPQueryError(samples.err))
	}
	if count.err != nil {
		return unavailableSeries(def.Key, step, normalizePCPQueryError(count.err))
	}

	points := buildCPUUsagePoints(samples.pts, count.val)
	if len(points) == 0 {
		return unavailableSeries(def.Key, step, pcpHistoryUnavailableReason)
	}

	return SeriesResponse{
		Available:   true,
		Range:       def.Key,
		StepSeconds: step,
		Points:      points,
	}
}

func GetMemorySeries(ctx context.Context, rangeKey string) SeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableSeries(rangeKey, 0, "unsupported monitoring range")
	}
	step := int(def.Step / time.Second)

	type result struct {
		pts []SeriesPoint
		err error
	}

	availCh := make(chan result, 1)
	physCh := make(chan result, 1)

	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric: pcpMemoryMetricAvailable,
			Range:  def,
		})
		availCh <- result{pts, err}
	}()
	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric: pcpMemoryMetricPhysical,
			Range:  def,
		})
		physCh <- result{pts, err}
	}()

	avail := <-availCh
	phys := <-physCh

	if avail.err != nil {
		return unavailableSeries(def.Key, step, normalizePCPQueryError(avail.err))
	}
	if phys.err != nil {
		return unavailableSeries(def.Key, step, normalizePCPQueryError(phys.err))
	}

	physicalMemory, ok := latestSampleValue(phys.pts)
	if !ok {
		return unavailableSeries(def.Key, step, pcpHistoryUnavailableReason)
	}

	points := buildMemoryUsagePoints(avail.pts, physicalMemory)
	if len(points) == 0 {
		return unavailableSeries(def.Key, step, pcpHistoryUnavailableReason)
	}

	return SeriesResponse{
		Available:   true,
		Range:       def.Key,
		StepSeconds: step,
		Points:      points,
	}
}

func GetGPUSeries(_ context.Context, rangeKey string) SeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableSeries(rangeKey, 0, "unsupported monitoring range")
	}

	return unavailableSeries(def.Key, int(def.Step/time.Second), pcpGPUReason)
}

func GetNetworkSeries(ctx context.Context, rangeKey, device string) NetworkSeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableNetworkSeries(rangeKey, 0, "unsupported monitoring range")
	}
	step := int(def.Step / time.Second)

	instances, err := resolveMetricInstances(ctx, pcpNetworkMetricReceive, device, func(name string) bool {
		return !pcpNetworkExcludeRE.MatchString(name)
	})
	if err != nil {
		return unavailableNetworkSeries(def.Key, step, normalizePCPQueryError(err))
	}
	if len(instances) == 0 {
		return unavailableNetworkSeries(
			def.Key,
			step,
			"waiting for PCP network samples for this interface",
		)
	}

	type result struct {
		pts []SeriesPoint
		err error
	}

	rxCh := make(chan result, 1)
	txCh := make(chan result, 1)

	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric:    pcpNetworkMetricReceive,
			Instances: instances,
			Range:     def,
			NoInterp:  true,
		})
		rxCh <- result{pts, err}
	}()
	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric:    pcpNetworkMetricTransmit,
			Instances: instances,
			Range:     def,
			NoInterp:  true,
		})
		txCh <- result{pts, err}
	}()

	rx := <-rxCh
	tx := <-txCh

	if rx.err != nil {
		return unavailableNetworkSeries(def.Key, step, normalizePCPQueryError(rx.err))
	}
	if tx.err != nil {
		return unavailableNetworkSeries(def.Key, step, normalizePCPQueryError(tx.err))
	}

	rxPoints, txPoints := alignSeriesPoints(
		downsampleSeriesPoints(buildCounterRatePoints(rx.pts, 1024), def.Step),
		downsampleSeriesPoints(buildCounterRatePoints(tx.pts, 1024), def.Step),
	)
	if len(rxPoints) == 0 && len(txPoints) == 0 {
		return unavailableNetworkSeries(
			def.Key,
			step,
			"waiting for PCP network samples for this interface",
		)
	}

	return NetworkSeriesResponse{
		Available:   true,
		Range:       def.Key,
		StepSeconds: step,
		RXPoints:    rxPoints,
		TXPoints:    txPoints,
	}
}

func GetDiskIOSeries(ctx context.Context, rangeKey, device string) DiskIOSeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableDiskIOSeries(rangeKey, 0, "unsupported monitoring range")
	}
	step := int(def.Step / time.Second)

	instances, err := resolveMetricInstances(ctx, pcpDiskMetricRead, device, pcpDiskDeviceRE.MatchString)
	if err != nil {
		return unavailableDiskIOSeries(def.Key, step, normalizePCPQueryError(err))
	}
	if len(instances) == 0 {
		return unavailableDiskIOSeries(
			def.Key,
			step,
			"waiting for PCP disk I/O samples",
		)
	}

	type result struct {
		pts []SeriesPoint
		err error
	}

	readCh := make(chan result, 1)
	writeCh := make(chan result, 1)

	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric:     pcpDiskMetricRead,
			Instances:  instances,
			Range:      def,
			ExtraCount: 1,
		})
		readCh <- result{pts, err}
	}()
	go func() {
		pts, err := queryPCPSamples(ctx, pcpSamplesRequest{
			Metric:     pcpDiskMetricWrite,
			Instances:  instances,
			Range:      def,
			ExtraCount: 1,
		})
		writeCh <- result{pts, err}
	}()

	rd := <-readCh
	wr := <-writeCh

	if rd.err != nil {
		return unavailableDiskIOSeries(def.Key, step, normalizePCPQueryError(rd.err))
	}
	if wr.err != nil {
		return unavailableDiskIOSeries(def.Key, step, normalizePCPQueryError(wr.err))
	}

	readPoints, writePoints := alignSeriesPoints(
		buildCounterRatePoints(rd.pts, 1.0/1024.0),
		buildCounterRatePoints(wr.pts, 1.0/1024.0),
	)
	if len(readPoints) == 0 && len(writePoints) == 0 {
		return unavailableDiskIOSeries(
			def.Key,
			step,
			"waiting for PCP disk I/O samples",
		)
	}

	return DiskIOSeriesResponse{
		Available:   true,
		Range:       def.Key,
		StepSeconds: step,
		ReadPoints:  readPoints,
		WritePoints: writePoints,
	}
}

func resolveMetricInstances(ctx context.Context, metric, device string, keep func(string) bool) ([]string, error) {
	if device != "" {
		return []string{device}, nil
	}

	instanceNames, err := queryPCPInstanceNames(ctx, metric)
	if err != nil {
		return nil, err
	}

	filtered := make([]string, 0, len(instanceNames))
	for _, name := range instanceNames {
		if keep(name) {
			filtered = append(filtered, name)
		}
	}
	slices.Sort(filtered)
	return filtered, nil
}

func buildCPUUsagePoints(idleSamples []SeriesPoint, cpuCount float64) []SeriesPoint {
	if cpuCount <= 0 || len(idleSamples) < 2 {
		return nil
	}

	points := make([]SeriesPoint, 0, len(idleSamples)-1)
	for i := 1; i < len(idleSamples); i++ {
		prev := idleSamples[i-1]
		curr := idleSamples[i]

		elapsedMs := float64(curr.TS - prev.TS)
		if elapsedMs <= 0 {
			continue
		}

		delta := curr.Value - prev.Value
		if delta < 0 {
			delta = 0
		}

		idlePercent := 100 * delta / (elapsedMs * cpuCount)
		usagePercent := clampFloat(100-idlePercent, 0, 100)
		points = append(points, SeriesPoint{TS: curr.TS, Value: usagePercent})
	}

	return points
}

func buildMemoryUsagePoints(availableSamples []SeriesPoint, physicalMemory float64) []SeriesPoint {
	if physicalMemory <= 0 || len(availableSamples) == 0 {
		return nil
	}

	points := make([]SeriesPoint, 0, len(availableSamples))
	for _, sample := range availableSamples {
		usagePercent := 100 * ((physicalMemory - sample.Value) / physicalMemory)
		points = append(points, SeriesPoint{
			TS:    sample.TS,
			Value: clampFloat(usagePercent, 0, 100),
		})
	}

	return points
}

func buildCounterRatePoints(samples []SeriesPoint, scale float64) []SeriesPoint {
	if len(samples) < 2 || scale <= 0 {
		return nil
	}

	points := make([]SeriesPoint, 0, len(samples)-1)
	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		curr := samples[i]

		elapsedSeconds := float64(curr.TS-prev.TS) / 1000
		if elapsedSeconds <= 0 {
			continue
		}

		delta := curr.Value - prev.Value
		if delta < 0 {
			delta = 0
		}

		points = append(points, SeriesPoint{
			TS:    curr.TS,
			Value: delta / elapsedSeconds / scale,
		})
	}

	return points
}

func downsampleSeriesPoints(points []SeriesPoint, step time.Duration) []SeriesPoint {
	if len(points) == 0 || step <= 0 {
		return points
	}

	stepMs := step.Milliseconds()
	if stepMs <= 0 {
		return points
	}

	type bucket struct {
		ts    int64
		sum   float64
		count int
	}

	buckets := make([]bucket, 0, len(points))
	currentBucketKey := int64(-1)
	for _, point := range points {
		bucketKey := point.TS / stepMs
		if len(buckets) == 0 || bucketKey != currentBucketKey {
			buckets = append(buckets, bucket{
				ts:    point.TS,
				sum:   point.Value,
				count: 1,
			})
			currentBucketKey = bucketKey
			continue
		}

		last := &buckets[len(buckets)-1]
		last.ts = point.TS
		last.sum += point.Value
		last.count++
	}

	result := make([]SeriesPoint, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, SeriesPoint{
			TS:    bucket.ts,
			Value: bucket.sum / float64(bucket.count),
		})
	}

	return result
}

func getCachedArchivePath() (string, error) {
	cachedArchivePathMu.Lock()
	defer cachedArchivePathMu.Unlock()
	if cachedArchivePath != "" && time.Since(cachedArchivePathAt) < archiveCacheTTL {
		return cachedArchivePath, nil
	}
	path, err := resolvePCPArchivePath()
	if err != nil {
		return "", err
	}
	cachedArchivePath = path
	cachedArchivePathAt = time.Now()
	return path, nil
}

func lookupRange(key string) (rangeDefinition, bool) {
	if key == "" {
		key = defaultRangeKey
	}
	def, ok := rangeDefinitions[key]
	return def, ok
}

func unavailableSeries(rangeKey string, stepSeconds int, reason string) SeriesResponse {
	if rangeKey == "" {
		rangeKey = defaultRangeKey
	}
	return SeriesResponse{
		Available:   false,
		Range:       rangeKey,
		StepSeconds: stepSeconds,
		Points:      []SeriesPoint{},
		Reason:      reason,
	}
}

func unavailableNetworkSeries(rangeKey string, stepSeconds int, reason string) NetworkSeriesResponse {
	if rangeKey == "" {
		rangeKey = defaultRangeKey
	}
	return NetworkSeriesResponse{
		Available:   false,
		Range:       rangeKey,
		StepSeconds: stepSeconds,
		RXPoints:    []SeriesPoint{},
		TXPoints:    []SeriesPoint{},
		Reason:      reason,
	}
}

func unavailableDiskIOSeries(rangeKey string, stepSeconds int, reason string) DiskIOSeriesResponse {
	if rangeKey == "" {
		rangeKey = defaultRangeKey
	}
	return DiskIOSeriesResponse{
		Available:   false,
		Range:       rangeKey,
		StepSeconds: stepSeconds,
		ReadPoints:  []SeriesPoint{},
		WritePoints: []SeriesPoint{},
		Reason:      reason,
	}
}

func alignSeriesPoints(primaryPoints, secondaryPoints []SeriesPoint) ([]SeriesPoint, []SeriesPoint) {
	if len(primaryPoints) == 0 && len(secondaryPoints) == 0 {
		return nil, nil
	}

	primaryByTimestamp := make(map[int64]float64, len(primaryPoints))
	for _, point := range primaryPoints {
		primaryByTimestamp[point.TS] = point.Value
	}

	secondaryByTimestamp := make(map[int64]float64, len(secondaryPoints))
	for _, point := range secondaryPoints {
		secondaryByTimestamp[point.TS] = point.Value
	}

	timestamps := make([]int64, 0, len(primaryByTimestamp)+len(secondaryByTimestamp))
	seen := make(map[int64]struct{}, len(primaryByTimestamp)+len(secondaryByTimestamp))
	for ts := range primaryByTimestamp {
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		timestamps = append(timestamps, ts)
	}
	for ts := range secondaryByTimestamp {
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		timestamps = append(timestamps, ts)
	}
	slices.Sort(timestamps)

	alignedPrimary := make([]SeriesPoint, 0, len(timestamps))
	alignedSecondary := make([]SeriesPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		alignedPrimary = append(alignedPrimary, SeriesPoint{
			TS:    ts,
			Value: primaryByTimestamp[ts],
		})
		alignedSecondary = append(alignedSecondary, SeriesPoint{
			TS:    ts,
			Value: secondaryByTimestamp[ts],
		})
	}

	return alignedPrimary, alignedSecondary
}

func normalizePCPQueryError(err error) string {
	if err == nil {
		return pcpHistorySetupReason
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return pcpHistoryTimeoutReason
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "cannot load libpcp"),
		strings.Contains(message, "cannot resolve"):
		return pcpHistoryLibReason
	case strings.Contains(message, "context deadline exceeded"),
		strings.Contains(message, "signal: killed"):
		return pcpHistoryTimeoutReason
	case strings.Contains(message, "archive"),
		strings.Contains(message, "no such file"),
		strings.Contains(message, "cannot open"),
		strings.Contains(message, "pmlogger"),
		strings.Contains(message, "pmnewcontext"),
		strings.Contains(message, "pmlookup"),
		strings.Contains(message, "pmapi error"):
		return pcpHistorySetupReason
	default:
		return pcpHistorySetupReason
	}
}

func detectPCPArchivePath() (string, error) {
	if archivePath := strings.TrimSpace(os.Getenv(pcpArchiveEnv)); archivePath != "" {
		return archivePath, nil
	}

	logDir := defaultPCPLogDir
	if configuredLogDir := readPCPConfigValue(pcpConfigPath, "PCP_LOG_DIR"); configuredLogDir != "" {
		logDir = configuredLogDir
	}

	archivePath, err := findPCPArchivePath(logDir)
	if err != nil {
		return "", fmt.Errorf("failed to find PCP archive path: %w", err)
	}
	return archivePath, nil
}

func findPCPArchivePath(logDir string) (string, error) {
	pattern := filepath.Join(logDir, "pmlogger", "*")
	candidates, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no pmlogger archive directories found under %s", pattern)
	}

	hostname, _ := os.Hostname()
	shortHostname, _, _ := strings.Cut(hostname, ".")

	type archiveCandidate struct {
		path    string
		modTime time.Time
	}

	usable := make([]archiveCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		usable = append(usable, archiveCandidate{path: candidate, modTime: info.ModTime()})
	}
	if len(usable) == 0 {
		return "", fmt.Errorf("no readable pmlogger archive directories found under %s", pattern)
	}

	for _, candidate := range usable {
		base := filepath.Base(candidate.path)
		if base == hostname || base == shortHostname {
			return candidate.path, nil
		}
	}

	slices.SortFunc(usable, func(a, b archiveCandidate) int {
		switch {
		case a.modTime.After(b.modTime):
			return -1
		case a.modTime.Before(b.modTime):
			return 1
		default:
			return strings.Compare(a.path, b.path)
		}
	})
	return usable[0].path, nil
}

func readPCPConfigValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	prefix := key + "="
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, prefix) {
			continue
		}
		value := strings.TrimPrefix(line, prefix)
		value = strings.Trim(value, `"'`)
		return strings.TrimSpace(value)
	}
	return ""
}

func latestSampleValue(samples []SeriesPoint) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	return samples[len(samples)-1].Value, true
}

func clampFloat(value, min, max float64) float64 {
	switch {
	case value < min:
		return min
	case value > max:
		return max
	default:
		return value
	}
}
