package system

import (
	"encoding/json"
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
