package plugins

import (
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// notSnapshotted are the plugins Snapshot does not project from a collected
// sample, each with where its payload comes from instead.
var notSnapshotted = map[string]string{
	Processes: "built by the live process collector",
	Programs:  "built by the live process collector",
	Smart:     "held by the app and stored by the store's SMART path",
}

// lastSampleWins are the plugins Average does not average: a bucket keeps the
// newest payload.
var lastSampleWins = map[string]string{
	Connections: "a table of the connections open at that moment",
	IRQ:         "cumulative per-CPU interrupt counters",
	Smart:       "a disk's health record, not a rate",
	Processes:   "live only, never stored",
	Programs:    "live only, never stored",
}

// sample returns collected data with every field the payloads carry set to n.
func sample(n int) *systemmodel.CombinedData {
	data := &systemmodel.CombinedData{}
	for _, field := range []any{&data.Stats, &data.Containers, &data.ContainerTelemetry, &data.Connections, &data.IRQs} {
		fillWith(reflect.ValueOf(field).Elem(), n)
	}
	return data
}

// fillWith sets v and everything below it to n (or "x"), with one element in
// each map and slice.
func fillWith(v reflect.Value, n int) {
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillWith(v.Field(i), n)
			}
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		elem := reflect.New(v.Type().Elem()).Elem()
		fillWith(elem, n)
		key := reflect.New(v.Type().Key()).Elem()
		fillWith(key, n)
		v.SetMapIndex(key, elem)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillWith(v.Index(0), n)
	case reflect.Array:
		for i := range v.Len() {
			fillWith(v.Index(i), n)
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillWith(v.Elem(), n)
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(n))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(n))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(n))
	default:
		panic("fillWith: unsupported kind " + v.Kind().String())
	}
}

func payloadJSON(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(raw)
}

func TestPluginTablesAgree(t *testing.T) {
	assert.Len(t, slices.Compact(slices.Sorted(slices.Values(names))), len(names), "a plugin is named twice")
	for plugin := range liveOnly {
		assert.True(t, IsName(plugin), "live-only plugin %s is not in names", plugin)
	}
	for _, plugin := range defaultHistory {
		assert.Contains(t, HistoryCapable(), plugin, "default history plugin %s cannot keep history", plugin)
	}

	snapshot := Snapshot(sample(1))
	for plugin := range snapshot {
		assert.True(t, IsName(plugin), "Snapshot projects %s, which is not in names", plugin)
	}
	for _, plugin := range names {
		_, projected := snapshot[plugin]
		_, other := notSnapshotted[plugin]
		assert.NotEqual(t, projected, other, "%s must be in Snapshot or in notSnapshotted, and not both", plugin)
	}
	for plugin := range notSnapshotted {
		assert.True(t, IsName(plugin), plugin)
	}
	for plugin := range lastSampleWins {
		assert.True(t, IsName(plugin), plugin)
	}
}

// Every metric a collector produces has to reach a payload: a Stats field that
// no plugin projects is never stored or served.
func TestSnapshotCarriesEveryStatsField(t *testing.T) {
	base := payloadJSON(t, Snapshot(&systemmodel.CombinedData{}))
	statsType := reflect.TypeFor[systemmodel.Stats]()
	for i := range statsType.NumField() {
		field := statsType.Field(i)
		if field.Tag.Get("json") == "-" {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			data := &systemmodel.CombinedData{}
			fillWith(reflect.ValueOf(&data.Stats).Elem().Field(i), 1)
			assert.NotEqual(t, base, payloadJSON(t, Snapshot(data)), "no plugin payload carries Stats.%s", field.Name)
		})
	}
}

// Averaging samples that are all alike has to give that sample back, field for
// field. A metric left out of the averaging, or out of the way a stored payload
// is read back into system stats, comes out zero.
func TestAverageKeepsEveryMetric(t *testing.T) {
	snapshot := Snapshot(sample(1))
	for _, plugin := range slices.Sorted(maps.Keys(snapshot)) {
		t.Run(plugin, func(t *testing.T) {
			payload := payloadJSON(t, snapshot[plugin])
			got, err := Average(plugin, []string{payload, payload})
			require.NoError(t, err)
			assert.JSONEq(t, payload, got)
		})
	}
}

// Average has a case for every plugin it is meant to average: one that falls
// through to the default keeps only the newest sample, so it is listed in
// lastSampleWins or it has to move when the samples differ.
func TestAverageAveragesEveryPluginNotListedAsLastSampleWins(t *testing.T) {
	low, high := Snapshot(sample(1)), Snapshot(sample(3))
	for _, plugin := range slices.Sorted(maps.Keys(low)) {
		if _, last := lastSampleWins[plugin]; last {
			continue
		}
		t.Run(plugin, func(t *testing.T) {
			newest := payloadJSON(t, high[plugin])
			got, err := Average(plugin, []string{payloadJSON(t, low[plugin]), newest})
			require.NoError(t, err)
			assert.NotEqual(t, newest, got, "Average keeps only the newest %s sample", plugin)
		})
	}
}
