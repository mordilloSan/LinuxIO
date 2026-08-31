package version

import "testing"

func TestGetTrimsInjectedBuildMetadata(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = originalVersion, originalCommit, originalDate
	})

	Version = "  v2.4.0\n"
	Commit = "\tabc123  "
	Date = " 2026-07-19T12:00:00Z "

	got := Get()
	if got.Version != "v2.4.0" {
		t.Errorf("Version = %q, want %q", got.Version, "v2.4.0")
	}
	if got.Commit != "abc123" {
		t.Errorf("Commit = %q, want %q", got.Commit, "abc123")
	}
	if got.Date != "2026-07-19T12:00:00Z" {
		t.Errorf("Date = %q, want %q", got.Date, "2026-07-19T12:00:00Z")
	}

	wantString := "indexer " + got.String()
	if gotString := String(); gotString != wantString {
		t.Errorf("String() = %q, want %q", gotString, wantString)
	}
}

func TestInfoString(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			name: "empty version defaults to dev",
			info: Info{Version: "", Commit: "", Date: "", Dirty: false},
			want: "dev",
		},
		{
			name: "version only",
			info: Info{Version: "v1.2.3", Commit: "", Date: "", Dirty: false},
			want: "v1.2.3",
		},
		{
			name: "commit",
			info: Info{Version: "v1.2.3", Commit: "abc123", Date: "", Dirty: false},
			want: "v1.2.3 (commit abc123)",
		},
		{
			name: "build date",
			info: Info{Version: "v1.2.3", Commit: "", Date: "2026-07-19", Dirty: false},
			want: "v1.2.3 (built 2026-07-19)",
		},
		{
			name: "dirty working tree",
			info: Info{Version: "v1.2.3", Commit: "", Date: "", Dirty: true},
			want: "v1.2.3 (dirty)",
		},
		{
			name: "all metadata in stable order",
			info: Info{
				Version: "v1.2.3",
				Commit:  "abc123",
				Date:    "2026-07-19T12:00:00Z",
				Dirty:   true,
			},
			want: "v1.2.3 (commit abc123, built 2026-07-19T12:00:00Z, dirty)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.info.String(); got != test.want {
				t.Fatalf("Info.String() = %q, want %q", got, test.want)
			}
		})
	}
}
