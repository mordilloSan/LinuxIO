package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGetCurrentFrequenciesUsesNumericCoreOrderAndGaps(t *testing.T) {
	root := t.TempDir()
	oldRoot := cpuSysfsRoot
	cpuSysfsRoot = root
	t.Cleanup(func() { cpuSysfsRoot = oldRoot })

	for name, value := range map[string]string{"cpu0": "2200000", "cpu2": "3300000", "cpu10": "4400000"} {
		path := filepath.Join(root, name, "cpufreq")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "scaling_cur_freq"), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := getCurrentFrequencies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{2200, 0, 3300, 0, 0, 0, 0, 0, 0, 0, 4400}; len(got) != len(want) {
		t.Fatalf("frequency length = %d, want %d (%v)", len(got), len(want), got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("frequency[%d] = %v, want %v (%v)", i, got[i], want[i], got)
			}
		}
	}
}
