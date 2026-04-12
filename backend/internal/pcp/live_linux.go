//go:build linux

package pcp

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
)

type MetricRequest struct {
	Name             string
	Instances        []string
	IncludeInstances bool
}

type MetricResult struct {
	Name      string
	Timestamp int64
	Value     float64
	Found     bool
	Instances map[string]float64
}

type FetchResult struct {
	Timestamp int64
	Metrics   map[string]MetricResult
}

type metricDef struct {
	pmid       uint32
	desc       MetricDesc
	instByName map[string]int32
	nameByInst map[int32]string
}

type fetchResponse struct {
	result FetchResult
	err    error
}

type fetchRequest struct {
	metrics []MetricRequest
	resp    chan fetchResponse
}

type LiveCollector struct {
	host  string
	reqCh chan fetchRequest
	done  chan struct{}
}

type liveContextState struct {
	host   string
	handle int32
	cache  map[string]metricDef
}

func NewLiveCollector(host string) (*LiveCollector, error) {
	if err := Ensure(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(host) == "" {
		host = "local:"
	}

	collector := &LiveCollector{
		host:  host,
		reqCh: make(chan fetchRequest),
		done:  make(chan struct{}),
	}
	go collector.loop()
	return collector, nil
}

func (c *LiveCollector) Close() {
	close(c.done)
}

func (c *LiveCollector) Fetch(metrics []MetricRequest) (FetchResult, error) {
	respCh := make(chan fetchResponse, 1)
	select {
	case <-c.done:
		return FetchResult{}, fmt.Errorf("collector is closed")
	case c.reqCh <- fetchRequest{metrics: metrics, resp: respCh}:
	}

	select {
	case <-c.done:
		return FetchResult{}, fmt.Errorf("collector is closed")
	case response := <-respCh:
		return response.result, response.err
	}
}

func newLiveContextState(host string) *liveContextState {
	return &liveContextState{
		host:   host,
		handle: -1,
		cache:  map[string]metricDef{},
	}
}

func (s *liveContextState) close() {
	if s.handle >= 0 {
		_pmDestroyContext(s.handle)
		s.handle = -1
	}
}

func (s *liveContextState) reopen() error {
	s.close()

	nextHandle, err := newContext(pmContextHost, s.host)
	if err != nil {
		return err
	}

	s.handle = nextHandle
	s.cache = map[string]metricDef{}
	return nil
}

func (s *liveContextState) ensureOpen() error {
	if s.handle >= 0 {
		return nil
	}
	return s.reopen()
}

func (c *LiveCollector) handleFetchRequest(state *liveContextState, req fetchRequest) {
	if err := state.ensureOpen(); err != nil {
		req.resp <- fetchResponse{err: err}
		return
	}

	result, err := c.fetchWithContext(state.cache, req.metrics)
	if err == nil {
		req.resp <- fetchResponse{result: result}
		return
	}

	if reopenErr := state.reopen(); reopenErr != nil {
		req.resp <- fetchResponse{err: reopenErr}
		return
	}

	result, err = c.fetchWithContext(state.cache, req.metrics)
	req.resp <- fetchResponse{result: result, err: err}
}

func (c *LiveCollector) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	state := newLiveContextState(c.host)
	defer state.close()

	for {
		select {
		case <-c.done:
			return
		case req := <-c.reqCh:
			c.handleFetchRequest(state, req)
		}
	}
}

func (c *LiveCollector) fetchWithContext(cache map[string]metricDef, metrics []MetricRequest) (FetchResult, error) {
	if len(metrics) == 0 {
		return FetchResult{Metrics: map[string]MetricResult{}}, nil
	}

	pmids := make([]uint32, 0, len(metrics))
	defs := make([]metricDef, 0, len(metrics))
	for _, metric := range metrics {
		def, ok := cache[metric.Name]
		if !ok {
			loaded, err := loadMetricDef(metric.Name)
			if err != nil {
				return FetchResult{}, err
			}
			cache[metric.Name] = loaded
			def = loaded
		}
		pmids = append(pmids, def.pmid)
		defs = append(defs, def)
	}

	result, err := fetchPMIDs(pmids)
	if err != nil {
		return FetchResult{}, err
	}
	defer result.free()

	ts := result.timestamp().UnixMilli()
	response := FetchResult{
		Timestamp: ts,
		Metrics:   make(map[string]MetricResult, len(metrics)),
	}

	for idx, metric := range metrics {
		def := defs[idx]
		filter := buildLiveInstanceFilter(metric.Instances, def.instByName)
		value, found := result.extractSumAt(idx, def.desc, filter)
		item := MetricResult{
			Name:      metric.Name,
			Timestamp: ts,
			Value:     value,
			Found:     found,
		}
		if metric.IncludeInstances && def.desc.InDom != pmIndomNull {
			instances, ok := result.extractInstancesAt(idx, def.desc, filter, def.nameByInst)
			if ok {
				item.Instances = instances
				item.Found = item.Found || len(instances) > 0
			}
		}
		response.Metrics[metric.Name] = item
	}

	return response, nil
}

func loadMetricDef(name string) (metricDef, error) {
	pmid, err := LookupPMID(name)
	if err != nil {
		return metricDef{}, err
	}
	desc, err := LookupDesc(pmid)
	if err != nil {
		return metricDef{}, err
	}

	def := metricDef{
		pmid: pmid,
		desc: desc,
	}
	if desc.InDom == pmIndomNull {
		return def, nil
	}

	instByName, err := GetInDomMap(desc.InDom)
	if err != nil {
		return metricDef{}, err
	}
	nameByInst := make(map[int32]string, len(instByName))
	for name, inst := range instByName {
		nameByInst[inst] = name
	}
	def.instByName = instByName
	def.nameByInst = nameByInst
	return def, nil
}

func buildLiveInstanceFilter(instances []string, instByName map[string]int32) map[int32]bool {
	if len(instances) == 0 || len(instByName) == 0 {
		return nil
	}
	filter := make(map[int32]bool, len(instances))
	for _, name := range instances {
		inst, ok := instByName[name]
		if ok {
			filter[inst] = true
		}
	}
	return filter
}

func FilterInstanceNames(names []string, allowed func(string) bool) []string {
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if allowed == nil || allowed(name) {
			filtered = append(filtered, name)
		}
	}
	slices.Sort(filtered)
	return filtered
}
