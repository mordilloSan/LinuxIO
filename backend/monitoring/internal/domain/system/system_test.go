package system

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

func TestUint8SliceMarshalJSONEncodesNumericArray(t *testing.T) {
	got, err := json.Marshal(Uint8Slice{0, 87, 100})
	if err != nil {
		t.Fatal(err)
	}

	const want = `[0,87,100]`
	if string(got) != want {
		t.Fatalf("json.Marshal(Uint8Slice) = %s, want %s", got, want)
	}
}

func TestUint8SliceMarshalJSONEncodesNilAsNull(t *testing.T) {
	got, err := json.Marshal(Uint8Slice(nil))
	if err != nil {
		t.Fatal(err)
	}

	const want = `null`
	if string(got) != want {
		t.Fatalf("json.Marshal(nil Uint8Slice) = %s, want %s", got, want)
	}
}

func TestUint8SliceUnmarshalJSONReadsNumericArray(t *testing.T) {
	var got Uint8Slice
	if err := json.Unmarshal([]byte(`[0,87,100]`), &got); err != nil {
		t.Fatal(err)
	}
	if want := (Uint8Slice{0, 87, 100}); string(got) != string(want) {
		t.Fatalf("Unmarshal = %v, want %v", got, want)
	}
	if err := json.Unmarshal([]byte(`[256]`), &got); err == nil {
		t.Fatal("Unmarshal([256]) succeeded, want overflow error")
	}
}

func TestStatsJSONUsesNumericCPUCoreUsage(t *testing.T) {
	got, err := json.Marshal(Stats{
		Cpu:           12.5,
		CpuCoresUsage: Uint8Slice{1, 2, 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}

	cores, ok := decoded["cpu_cores_percent"].([]any)
	if !ok {
		t.Fatalf("cpu_cores_percent decoded as %T, want JSON array; payload: %s", decoded["cpu_cores_percent"], got)
	}
	if len(cores) != 3 || cores[0] != float64(1) || cores[1] != float64(2) || cores[2] != float64(3) {
		t.Fatalf("cpu_cores_percent = %#v, want [1 2 3]", cores)
	}
}

func TestStatsBatteryUsesNumericArray(t *testing.T) {
	var stats Stats
	if err := json.Unmarshal([]byte(`{"battery":[85,1]}`), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Battery != [2]uint16{85, 1} {
		t.Fatalf("Battery = %v, want [85 1]", stats.Battery)
	}

	got, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"battery":[85,1]`; !strings.Contains(string(got), want) {
		t.Fatalf("json.Marshal(Stats) = %s, want it to contain %s", got, want)
	}
}
