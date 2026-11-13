package dockerupdate

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"strings"
	"testing"
)

// TestAutoUpdateStoreRoundTripPersistsEveryOption fails when a saved option is
// not written to or read back from the config file, or when the file is not
// exactly version plus the saved options. Saved options live in
// DockerContainerAutoUpdateSettings; Enabled mirrors the systemd timer and is
// never stored in the file.
func TestAutoUpdateStoreRoundTripPersistsEveryOption(t *testing.T) {
	var want DockerContainerAutoUpdateOptions
	options := reflect.TypeFor[DockerContainerAutoUpdateOptions]()
	for field := range options.Fields() {
		if field.Name != "Enabled" && field.Type != reflect.TypeFor[DockerContainerAutoUpdateSettings]() {
			t.Fatalf("option %s must be a field of DockerContainerAutoUpdateSettings so it is saved", field.Name)
		}
	}
	want.Enabled = true

	var saved []string
	v := reflect.ValueOf(&want.DockerContainerAutoUpdateSettings).Elem()
	for i := range v.NumField() {
		field := v.Type().Field(i)
		saved = append(saved, strings.Split(field.Tag.Get("json"), ",")[0])
		f := v.Field(i)
		switch {
		case field.Name == "Mode":
			f.SetString("check_only")
		case field.Name == "Time":
			f.SetString("06:15")
		case f.Kind() == reflect.Bool:
			f.SetBool(true)
		case f.Type() == reflect.TypeFor[[]string]():
			f.Set(reflect.ValueOf([]string{"app", "redis"}))
		default:
			t.Fatalf("set a valid non-zero value for new option %s (%s) in this test", field.Name, f.Type())
		}
	}

	store := testAutoUpdateStore(t)
	if err := store.writeOptions(want); err != nil {
		t.Fatalf("writeOptions: %v", err)
	}

	var members map[string]jsontext.Value
	if err := json.Unmarshal([]byte(readTestFile(t, store.configPath)), &members); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if _, ok := members["enabled"]; ok {
		t.Fatal("config file must not contain enabled")
	}
	if _, ok := members["version"]; !ok {
		t.Fatal("config file has no version")
	}
	for _, name := range saved {
		if _, ok := members[name]; !ok {
			t.Fatalf("config file has no %q member", name)
		}
	}
	if len(members) != len(saved)+1 {
		t.Fatalf("config file has %d members, want version plus %d saved options", len(members), len(saved))
	}

	got, err := store.readOptions()
	if err != nil {
		t.Fatalf("readOptions: %v", err)
	}
	want.Enabled = false
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip lost options:\n got  %+v\n want %+v", got, want)
	}
}

func TestNormalizeContainerAutoUpdateOptionsKeepsEveryOption(t *testing.T) {
	want := DockerContainerAutoUpdateOptions{
		Cleanup:        true,
		ContainerNames: []string{"app"},
		IncludeStopped: true,
		Mode:           "check_only",
		ReviveStopped:  true,
		Time:           "06:15",
		UpdateAll:      true,
		UpdateStopped:  true,
		Enabled:        true,
	}
	got, err := normalizeContainerAutoUpdateOptions(want)
	if err != nil {
		t.Fatalf("normalizeContainerAutoUpdateOptions: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize changed valid options:\n got  %+v\n want %+v", got, want)
	}
}
