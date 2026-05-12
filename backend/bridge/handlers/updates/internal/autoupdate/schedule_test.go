package autoupdate

import "testing"

func TestOnCalendarFor(t *testing.T) {
	tests := []struct {
		freq    string
		want    string
		wantErr bool
	}{
		{freq: "hourly", want: "hourly"},
		{freq: "daily", want: "daily"},
		{freq: "weekly", want: "weekly"},
		{freq: "monthly", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.freq, func(t *testing.T) {
			got, err := onCalendarFor(tc.freq)
			if tc.wantErr {
				if err == nil {
					t.Fatal("onCalendarFor returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("onCalendarFor: %v", err)
			}
			if got != tc.want {
				t.Fatalf("onCalendarFor(%q) = %q, want %q", tc.freq, got, tc.want)
			}
		})
	}
}
