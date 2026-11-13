package httpapi

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

// liveMembersEveryListenerMaySee are the members of monitoringapi.Live that
// redactLiveSections leaves alone, each with its reason. Any other member is
// a data section and must be emptied for a listener without its plugin.
var liveMembersEveryListenerMaySee = map[string]string{
	"CapturedAtMs":  "the capture time says how fresh the payload is and carries no host data",
	"UptimeSeconds": "uptime is part of the heartbeat every listener gets",
}

// TestRedactLiveSectionsCoversEveryLiveMember fails when a member added to
// monitoringapi.Live is neither emptied by redactLiveSections for a listener
// that is allowed no plugin nor listed above.
func TestRedactLiveSectionsCoversEveryLiveMember(t *testing.T) {
	liveType := reflect.TypeFor[monitoringapi.Live]()
	for i := range liveType.NumField() {
		name := liveType.Field(i).Name
		t.Run(name, func(t *testing.T) {
			var live monitoringapi.Live
			field := reflect.ValueOf(&live).Elem().Field(i)
			fillNonZero(field)

			redactLiveSections(&live, func(string) bool { return false })

			if reason, ok := liveMembersEveryListenerMaySee[name]; ok {
				assert.NotEmpty(t, reason)
				assert.False(t, isEmptyValue(field), "%s is listed as visible to every listener but was emptied", name)
				return
			}
			assert.True(t, isEmptyValue(field), "Live.%s reaches a listener without its plugin: redact it in redactLiveSections or list it in liveMembersEveryListenerMaySee", name)
		})
	}
}

// TestRedactLiveSectionsKeepsPermittedSections checks the redaction leaves
// everything alone for a listener that is allowed every section.
func TestRedactLiveSectionsKeepsPermittedSections(t *testing.T) {
	var live monitoringapi.Live
	fillNonZero(reflect.ValueOf(&live).Elem())
	want := live

	redactLiveSections(&live, func(string) bool { return true })

	assert.Equal(t, want, live)
}

// fillNonZero sets v and everything below it to non-zero values, with one
// element in each map and slice.
func fillNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for _, field := range v.Fields() {
			fillNonZero(field)
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		elem := reflect.New(v.Type().Elem()).Elem()
		fillNonZero(elem)
		key := reflect.New(v.Type().Key()).Elem()
		fillNonZero(key)
		v.SetMapIndex(key, elem)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillNonZero(v.Index(0))
	case reflect.Array:
		for i := range v.Len() {
			fillNonZero(v.Index(i))
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(v.Elem())
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	default:
		panic("fillNonZero: unsupported kind " + v.Kind().String())
	}
}

// isEmptyValue reports whether v carries no data: zero numbers and strings,
// and maps, slices and structs holding nothing but such values.
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Struct:
		for _, field := range v.Fields() {
			if !isEmptyValue(field) {
				return false
			}
		}
		return true
	case reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.Array:
		for i := range v.Len() {
			if !isEmptyValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Pointer:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}
