package httpapi

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store/storetest"
)

// The bridge decodes history stats into the monitoring/api payload types while
// the store writes its own plugin payloads. These tests pin the two to the same
// JSON keys: a renamed tag on either side leaves an api field zero.

func assertEveryFieldDecoded(t *testing.T, stored any, target any) {
	t.Helper()
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	assertEveryFieldDecodedFrom(t, raw, target)
}

func assertEveryFieldDecodedFrom(t *testing.T, raw []byte, target any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(raw, target))
	decoded := reflect.ValueOf(target).Elem()
	for i := range decoded.NumField() {
		assert.Falsef(t, decoded.Field(i).IsZero(), "%s.%s was not decoded from %s", decoded.Type(), decoded.Type().Field(i).Name, raw)
	}
}

func TestHistoryStatsPayloadsMatchTheStore(t *testing.T) {
	data := storetest.SampleCombinedData(55)
	data.Stats.CpuBreakdown = []float64{1, 2, 3, 4, 5}
	data.Stats.CpuCoresUsage = []uint8{10, 20}
	data.Stats.CpuFrequencies = []float64{3200, 3300}
	data.Stats.MemZfsArc = 1.5
	data.Stats.DiskIO = [2]uint64{100, 200}
	data.Stats.NetworkInterfaces = map[string][4]uint64{"eth0": {1, 2, 3, 4}}
	payloads := plugins.Snapshot(data)

	var cpu monitoringapi.CPUHistoryStats
	assertEveryFieldDecoded(t, payloads[plugins.CPU], &cpu)
	var mem monitoringapi.MemHistoryStats
	assertEveryFieldDecoded(t, payloads[plugins.Mem], &mem)
	var diskIO monitoringapi.DiskIOHistoryStats
	assertEveryFieldDecoded(t, payloads[plugins.DiskIO], &diskIO)
	var network monitoringapi.NetworkHistoryStats
	assertEveryFieldDecoded(t, payloads[plugins.Network], &network)

	raw, err := json.Marshal(payloads[plugins.Containers])
	require.NoError(t, err)
	var records []jsontext.Value
	require.NoError(t, json.Unmarshal(raw, &records))
	require.Len(t, records, 1)
	var container monitoringapi.ContainerHistoryRecord
	assertEveryFieldDecodedFrom(t, records[0], &container)
}

func TestHistoryResolutionsAreAllQueryable(t *testing.T) {
	for _, resolution := range monitoringapi.HistoryResolutions() {
		assert.Truef(t, store.ValidResolution(resolution), "store rejects shared resolution %q", resolution)
		_, ok := monitoringapi.HistoryResolutionStep(resolution)
		assert.True(t, ok, resolution)
	}
	assert.False(t, store.ValidResolution("5m"))
}

// The daemon forwards process payloads as raw JSON; the bridge decodes them
// into the shared envelopes, strictly here so an added or renamed key fails.
func TestProcessEnvelopesMatchTheSharedTypes(t *testing.T) {
	data := storetest.SampleCombinedData(55)
	processes, err := json.Marshal(rawProcessesResponse{
		CapturedAt: 7,
		Count:      mustMarshal(t, data.ProcessCount),
		Items:      mustMarshal(t, data.Processes),
	})
	require.NoError(t, err)
	var processList monitoringapi.ProcessListResponse
	decodeStrict(t, processes, &processList)
	assert.Equal(t, int64(7), processList.CapturedAt)
	assert.Equal(t, *data.ProcessCount, processList.Count)
	assert.Len(t, processList.Items, 1)

	programs, err := json.Marshal(rawItemsResponse{CapturedAt: 8, Items: mustMarshal(t, data.Programs)})
	require.NoError(t, err)
	var programList monitoringapi.ProgramListResponse
	decodeStrict(t, programs, &programList)
	assert.Equal(t, int64(8), programList.CapturedAt)
	assert.Len(t, programList.Items, 1)
}

func mustMarshal(t *testing.T, v any) jsontext.Value {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func decodeStrict(t *testing.T, raw []byte, target any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(target))
}
