package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/docker/docker/client"
)

const (
	defaultRangeKey          = "1m"
	prometheusContainerName  = "linuxio-prometheus"
	prometheusNetworkName    = "linuxio-docker"
	prometheusQueryPath      = "/api/v1/query_range"
	prometheusRequestTimeout = 5 * time.Second
)

const (
	cpuUsageQuery          = `100 * (1 - avg(rate(node_cpu_seconds_total{job="node",mode="idle"}[1m])))`
	memoryUsageQuery       = `100 * (1 - avg(node_memory_MemAvailable_bytes{job="node"} / node_memory_MemTotal_bytes{job="node"}))`
	gpuUsageQuery          = `100 * (gpu_utilization_ratio{job="gpu-scraper"} or max by (card, pci_slot, vendor) (gpu_intel_engine_utilization_ratio{job="gpu-scraper"}))`
	networkReceiveMetric   = "node_network_receive_bytes_total"
	networkTransmitMetric  = "node_network_transmit_bytes_total"
	networkRateWindow      = "15s"
	networkRateUnitDivisor = 1024
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

type prometheusQueryRangeResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string                 `json:"resultType"`
		Result     []prometheusMatrixItem `json:"result"`
	} `json:"data"`
}

type prometheusMatrixItem struct {
	Metric map[string]string       `json:"metric"`
	Values []prometheusSampleValue `json:"values"`
}

type prometheusSampleValue struct {
	Timestamp float64
	Value     string
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
	resolvePrometheusBaseURL = detectPrometheusBaseURL
	httpClient               = &http.Client{Timeout: prometheusRequestTimeout}
)

func (v *prometheusSampleValue) UnmarshalJSON(data []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &v.Timestamp); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[1], &v.Value); err != nil {
		return err
	}
	return nil
}

func GetCPUSeries(ctx context.Context, rangeKey string) SeriesResponse {
	return fetchSeries(ctx, rangeKey, cpuUsageQuery)
}

func GetMemorySeries(ctx context.Context, rangeKey string) SeriesResponse {
	return fetchSeries(ctx, rangeKey, memoryUsageQuery)
}

func GetGPUSeries(ctx context.Context, rangeKey string) SeriesResponse {
	return fetchSeries(ctx, rangeKey, gpuUsageQuery)
}

func GetNetworkSeries(ctx context.Context, rangeKey, device string) NetworkSeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableNetworkSeries(rangeKey, 0, "unsupported monitoring range")
	}
	baseURL, err := resolvePrometheusBaseURL(ctx)
	if err != nil {
		return unavailableNetworkSeries(def.Key, int(def.Step/time.Second), err.Error())
	}

	var rxQuery, txQuery string
	if device == "" {
		rxQuery = buildNetworkTotalQuery(networkReceiveMetric)
		txQuery = buildNetworkTotalQuery(networkTransmitMetric)
	} else {
		rxQuery = buildNetworkRateQuery(networkReceiveMetric, device)
		txQuery = buildNetworkRateQuery(networkTransmitMetric, device)
	}

	rxResponse, err := queryPrometheusRange(ctx, baseURL, def, rxQuery)
	if err != nil {
		return unavailableNetworkSeries(def.Key, int(def.Step/time.Second), err.Error())
	}

	txResponse, err := queryPrometheusRange(ctx, baseURL, def, txQuery)
	if err != nil {
		return unavailableNetworkSeries(def.Key, int(def.Step/time.Second), err.Error())
	}

	rxPoints, txPoints := alignSeriesPoints(
		normalizePrometheusPoints(rxResponse.Data.Result),
		normalizePrometheusPoints(txResponse.Data.Result),
	)
	if len(rxPoints) == 0 && len(txPoints) == 0 {
		return unavailableNetworkSeries(
			def.Key,
			int(def.Step/time.Second),
			"waiting for Prometheus network samples for this interface",
		)
	}

	return NetworkSeriesResponse{
		Available:   true,
		Range:       def.Key,
		StepSeconds: int(def.Step / time.Second),
		RXPoints:    rxPoints,
		TXPoints:    txPoints,
	}
}

func fetchSeries(ctx context.Context, rangeKey, query string) SeriesResponse {
	def, ok := lookupRange(rangeKey)
	if !ok {
		return unavailableSeries(rangeKey, 0, "unsupported monitoring range")
	}

	baseURL, err := resolvePrometheusBaseURL(ctx)
	if err != nil {
		return unavailableSeries(def.Key, int(def.Step/time.Second), err.Error())
	}

	response, err := queryPrometheusRange(ctx, baseURL, def, query)
	if err != nil {
		return unavailableSeries(def.Key, int(def.Step/time.Second), err.Error())
	}

	points := normalizePrometheusPoints(response.Data.Result)
	if len(points) == 0 {
		return unavailableSeries(def.Key, int(def.Step/time.Second), "no historical samples available yet")
	}

	return SeriesResponse{
		Available:   true,
		Range:       def.Key,
		StepSeconds: int(def.Step / time.Second),
		Points:      points,
	}
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

func buildNetworkRateQuery(metric, device string) string {
	return fmt.Sprintf(
		`clamp_min(rate(%s{job="node",device=%q}[%s]), 0) / %d`,
		metric,
		device,
		networkRateWindow,
		networkRateUnitDivisor,
	)
}

func buildNetworkTotalQuery(metric string) string {
	return fmt.Sprintf(
		`sum(clamp_min(rate(%s{job="node",device!~"veth.*|docker.*|br.*|lo"}[%s]), 0)) / %d`,
		metric,
		networkRateWindow,
		networkRateUnitDivisor,
	)
}

func queryPrometheusRange(
	ctx context.Context,
	baseURL string,
	def rangeDefinition,
	query string,
) (prometheusQueryRangeResponse, error) {
	var response prometheusQueryRangeResponse

	end := time.Now()
	start := end.Add(-def.Duration)

	values := url.Values{}
	values.Set("query", query)
	values.Set("start", strconv.FormatInt(start.Unix(), 10))
	values.Set("end", strconv.FormatInt(end.Unix(), 10))
	values.Set("step", def.Step.String())

	requestURL := fmt.Sprintf("%s%s?%s", baseURL, prometheusQueryPath, values.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return response, fmt.Errorf("failed to build Prometheus request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return response, fmt.Errorf("failed to query Prometheus: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response, fmt.Errorf("failed to read Prometheus response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return response, fmt.Errorf("prometheus returned HTTP %d", resp.StatusCode)
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return response, fmt.Errorf("failed to decode Prometheus response: %w", err)
	}

	if response.Status != "success" {
		if response.Error != "" {
			return response, fmt.Errorf("prometheus query failed: %s", response.Error)
		}
		return response, fmt.Errorf("prometheus query failed")
	}

	return response, nil
}

func normalizePrometheusPoints(items []prometheusMatrixItem) []SeriesPoint {
	if len(items) == 0 {
		return nil
	}

	buckets := make(map[int64][]float64)

	for _, item := range items {
		for _, sample := range item.Values {
			value, err := strconv.ParseFloat(sample.Value, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			ts := int64(math.Round(sample.Timestamp * 1000))
			buckets[ts] = append(buckets[ts], value)
		}
	}

	if len(buckets) == 0 {
		return nil
	}

	timestamps := make([]int64, 0, len(buckets))
	for ts := range buckets {
		timestamps = append(timestamps, ts)
	}
	slices.Sort(timestamps)

	points := make([]SeriesPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		values := buckets[ts]
		total := 0.0
		for _, value := range values {
			total += value
		}
		points = append(points, SeriesPoint{
			TS:    ts,
			Value: total / float64(len(values)),
		})
	}

	return points
}

func alignSeriesPoints(rxPoints, txPoints []SeriesPoint) ([]SeriesPoint, []SeriesPoint) {
	if len(rxPoints) == 0 && len(txPoints) == 0 {
		return nil, nil
	}

	rxByTS := make(map[int64]float64, len(rxPoints))
	for _, point := range rxPoints {
		rxByTS[point.TS] = point.Value
	}

	txByTS := make(map[int64]float64, len(txPoints))
	for _, point := range txPoints {
		txByTS[point.TS] = point.Value
	}

	timestamps := make([]int64, 0, len(rxByTS)+len(txByTS))
	seen := make(map[int64]struct{}, len(rxByTS)+len(txByTS))
	for ts := range rxByTS {
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		timestamps = append(timestamps, ts)
	}
	for ts := range txByTS {
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		timestamps = append(timestamps, ts)
	}
	slices.Sort(timestamps)

	alignedRX := make([]SeriesPoint, 0, len(timestamps))
	alignedTX := make([]SeriesPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		alignedRX = append(alignedRX, SeriesPoint{
			TS:    ts,
			Value: rxByTS[ts],
		})
		alignedTX = append(alignedTX, SeriesPoint{
			TS:    ts,
			Value: txByTS[ts],
		})
	}

	return alignedRX, alignedTX
}

func detectPrometheusBaseURL(ctx context.Context) (string, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, prometheusRequestTimeout)
	defer cancel()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return "", fmt.Errorf("docker client unavailable: %w", err)
	}
	defer cli.Close()

	containerJSON, err := cli.ContainerInspect(inspectCtx, prometheusContainerName)
	if err != nil {
		return "", fmt.Errorf("prometheus container not available")
	}

	if containerJSON.ContainerJSONBase == nil ||
		containerJSON.State == nil ||
		!containerJSON.State.Running {
		return "", fmt.Errorf("prometheus container is not running")
	}
	if containerJSON.NetworkSettings == nil {
		return "", fmt.Errorf("prometheus container has no network settings")
	}

	ipAddress := ""
	if network, ok := containerJSON.NetworkSettings.Networks[prometheusNetworkName]; ok && network != nil {
		ipAddress = network.IPAddress
	}
	if ipAddress == "" {
		for _, network := range containerJSON.NetworkSettings.Networks {
			if network != nil && network.IPAddress != "" {
				ipAddress = network.IPAddress
				break
			}
		}
	}
	if ipAddress == "" {
		return "", fmt.Errorf("prometheus container has no reachable IP address")
	}

	return fmt.Sprintf("http://%s:9090", ipAddress), nil
}
