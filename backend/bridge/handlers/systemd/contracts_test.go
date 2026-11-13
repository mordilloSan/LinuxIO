package systemd

import (
	"fmt"
	"reflect"
	"testing"
)

// TestStatusesToAPICarryEverySharedProperty fills every field of each status
// with a distinct value and checks that the API value sets every member and
// carries each UnitStatus field to the member of the same name.
func TestStatusesToAPICarryEverySharedProperty(t *testing.T) {
	var service ServiceStatus
	var timer TimerStatus
	var socket SocketStatus
	fillDistinct(t, &service)
	fillDistinct(t, &timer)
	fillDistinct(t, &socket)

	for _, tc := range []struct {
		shared UnitStatus
		api    any
	}{
		{service.UnitStatus, servicesToAPI([]ServiceStatus{service})[0]},
		{timer.UnitStatus, timersToAPI([]TimerStatus{timer})[0]},
		{socket.UnitStatus, socketsToAPI([]SocketStatus{socket})[0]},
	} {
		api := reflect.ValueOf(tc.api)
		requireEveryFieldSet(t, api)
		for field, want := range reflect.ValueOf(tc.shared).Fields() {
			got := api.FieldByName(field.Name)
			switch {
			case !got.IsValid():
				t.Errorf("%s has no %s field for UnitStatus.%[2]s", api.Type(), field.Name)
			case got.IsZero():
				// Reported above.
			case fmt.Sprint(reflect.Indirect(got)) != fmt.Sprint(want):
				t.Errorf("%s.%s = %v, want %v", api.Type(), field.Name, reflect.Indirect(got), want)
			}
		}
	}
}

// requireEveryFieldSet reports each zero field of v, looking inside embedded
// structs so a shared property left out of the copy is named.
func requireEveryFieldSet(t *testing.T, v reflect.Value) {
	t.Helper()
	for field, value := range v.Fields() {
		switch {
		case field.Anonymous && value.Kind() == reflect.Struct:
			requireEveryFieldSet(t, value)
		case value.IsZero():
			t.Errorf("%s.%s is not set", v.Type(), field.Name)
		}
	}
}

// fillDistinct sets every field of the struct ptr points to, including the
// fields of embedded structs, to a non-zero value no other field has.
func fillDistinct(t *testing.T, ptr any) {
	t.Helper()
	n := 0
	var fill func(reflect.Value)
	fill = func(v reflect.Value) {
		for field, value := range v.Fields() {
			n++
			switch {
			case field.Anonymous && value.Kind() == reflect.Struct:
				fill(value)
			case value.Kind() == reflect.String:
				value.SetString(fmt.Sprintf("%s-%d", field.Name, n))
			case value.CanInt():
				value.SetInt(int64(n))
			case value.CanUint():
				value.SetUint(uint64(n))
			case value.Kind() == reflect.Bool:
				value.SetBool(true)
			case value.Type() == reflect.TypeFor[[]string]():
				value.Set(reflect.ValueOf([]string{fmt.Sprintf("%s-%d", field.Name, n)}))
			default:
				t.Fatalf("fillDistinct: %s.%s has type %s; teach fillDistinct a value for it", v.Type(), field.Name, value.Type())
			}
		}
	}
	fill(reflect.ValueOf(ptr).Elem())
}
