package filebrowser

import (
	"encoding/json"
	"testing"
)

func TestSubfoldersFromIndexerUsesCanonicalSizeAndIgnoresBytes(t *testing.T) {
	var upstream []indexerSubfolder
	if err := json.Unmarshal([]byte(`[
		{"path":"/data/cache","name":"cache","size":0,"bytes":8192,"mod_time":"2026-07-30T12:00:00Z"}
	]`), &upstream); err != nil {
		t.Fatal(err)
	}

	got := subfoldersFromIndexer(upstream)
	if len(got) != 1 {
		t.Fatalf("subfolder count = %d, want 1", len(got))
	}
	if got[0].Size != 0 || got[0].Path != "/data/cache" || got[0].Name != "cache" || got[0].ModTime != "2026-07-30T12:00:00Z" {
		t.Fatalf("subfolder = %#v, want only canonical indexer fields", got[0])
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"mod_time":"2026-07-30T12:00:00Z","name":"cache","path":"/data/cache","size":0}`; string(encoded) != want {
		t.Fatalf("public subfolder JSON = %s, want %s (bytes must not escape)", encoded, want)
	}
}

func TestSubfoldersFromIndexerReturnsNonNilEmptySlice(t *testing.T) {
	got := subfoldersFromIndexer(nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("subfoldersFromIndexer(nil) = %#v, want non-nil empty slice", got)
	}
}

func TestSearchResultFromIndexerCanonicalizesTimestampAndPreservesFields(t *testing.T) {
	var upstream indexerSearchResult
	if err := json.Unmarshal([]byte(`{
		"path":"/data/report.txt","name":"report.txt","type":"file","isDir":false,
		"size":42,"inode":99,"mod_time":"canonical","modTime":"legacy-camel","modified":"legacy-old",
		"total_size":0,"total_files":0,"total_dirs":0
	}`), &upstream); err != nil {
		t.Fatal(err)
	}

	got := searchResultFromIndexer(upstream)
	if got.ModTime != "canonical" || got.Path != "/data/report.txt" || got.Name != "report.txt" || got.Type != "file" || got.IsDir || got.Size != 42 || got.Inode != 99 {
		t.Fatalf("search result = %#v, want canonical field preservation", got)
	}
	if got.TotalSize == nil || *got.TotalSize != 0 || got.TotalFiles == nil || *got.TotalFiles != 0 || got.TotalDirs == nil || *got.TotalDirs != 0 {
		t.Fatalf("zero total counters must remain present: %#v", got)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	if _, exists := public["modTime"]; exists {
		t.Fatalf("public result emitted legacy modTime alias: %s", encoded)
	}
	if _, exists := public["modified"]; exists {
		t.Fatalf("public result emitted legacy modified alias: %s", encoded)
	}
	if want := `{"inode":99,"isDir":false,"mod_time":"canonical","name":"report.txt","path":"/data/report.txt","size":42,"total_dirs":0,"total_files":0,"total_size":0,"type":"file"}`; string(encoded) != want {
		t.Fatalf("public search JSON = %s, want %s (legacy aliases must not escape)", encoded, want)
	}
}

func TestSearchResultFromIndexerTimestampAliasPrecedence(t *testing.T) {
	for _, test := range []struct {
		name string
		json string
		want string
	}{
		{"canonical", `{"mod_time":"canonical","modTime":"camel","modified":"old"}`, "canonical"},
		{"camel", `{"modTime":"camel","modified":"old"}`, "camel"},
		{"old", `{"modified":"old"}`, "old"},
		{"canonical empty still wins", `{"mod_time":"","modTime":"camel"}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var upstream indexerSearchResult
			if err := json.Unmarshal([]byte(test.json), &upstream); err != nil {
				t.Fatal(err)
			}
			if got := searchResultFromIndexer(upstream).ModTime; got != test.want {
				t.Fatalf("mod_time = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSearchResultFromIndexerNormalizesTypeAndDirectoryState(t *testing.T) {
	trueValue := true
	falseValue := false
	for _, test := range []struct {
		name     string
		typeName string
		isDir    *bool
		path     string
		wantType string
		wantDir  bool
	}{
		{"folder", "folder", &falseValue, "/a", "directory", true},
		{"dir", "dir", &falseValue, "/a", "directory", true},
		{"directory", "directory", &falseValue, "/a", "directory", true},
		{"file", "file", &trueValue, "/a", "file", false},
		{"custom preserves type", "symlink", &trueValue, "/a", "symlink", true},
		{"missing type legacy flag", "", &trueValue, "/a", "directory", true},
		{"missing type trailing slash", "", nil, "/a/", "directory", true},
		{"missing type file fallback", "", nil, "/a", "file", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := searchResultFromIndexer(indexerSearchResult{Type: test.typeName, IsDir: test.isDir, Path: test.path})
			if got.Type != test.wantType || got.IsDir != test.wantDir {
				t.Fatalf("normalized = (%q, %t), want (%q, %t)", got.Type, got.IsDir, test.wantType, test.wantDir)
			}
		})
	}
}

func TestSearchResponseFromIndexerReturnsNonNilEmptyResults(t *testing.T) {
	got := searchResponseFromIndexer("report", nil)
	if got.Results == nil || got.Count != 0 || got.Query != "report" {
		t.Fatalf("response = %#v, want non-nil empty results", got)
	}
}

func TestSearchResultFromIndexerLeavesMissingTotalsAbsent(t *testing.T) {
	got := searchResultFromIndexer(indexerSearchResult{})
	if got.TotalSize != nil || got.TotalFiles != nil || got.TotalDirs != nil {
		t.Fatalf("missing total counters = %#v, want nil", got)
	}
}
