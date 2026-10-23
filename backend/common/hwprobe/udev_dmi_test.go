package hwprobe

import "testing"

func TestHasUdevMemoryDeviceData(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]string
		want  bool
	}{
		{"empty", map[string]string{}, false},
		{"zero devices", map[string]string{"MEMORY_ARRAY_NUM_DEVICES": "0", "MEMORY_DEVICE_0_SIZE": "1"}, false},
		{"count without devices", map[string]string{"MEMORY_ARRAY_NUM_DEVICES": "2"}, false},
		{"device data", map[string]string{"MEMORY_ARRAY_NUM_DEVICES": "2", "MEMORY_DEVICE_0_SIZE": "1"}, true},
	}
	for _, tt := range tests {
		if got := HasUdevMemoryDeviceData(tt.props); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
