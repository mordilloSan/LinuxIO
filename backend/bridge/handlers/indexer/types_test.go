package indexer

import (
	"encoding/json"
	"testing"
)

func TestIndexerProgressOmitsUnsetCounters(t *testing.T) {
	payload, err := json.Marshal(IndexerProgress{
		Message: "Removing stale entries",
		Phase:   "indexing",
	})
	if err != nil {
		t.Fatalf("marshal progress: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	for _, field := range []string{"files_indexed", "dirs_indexed", "bytes_indexed"} {
		if _, ok := fields[field]; ok {
			t.Errorf("phase-only progress unexpectedly includes %q: %s", field, payload)
		}
	}
}

func TestIndexerProgressIncludesReportedCounters(t *testing.T) {
	payload, err := json.Marshal(IndexerProgress{
		BytesIndexed: 512,
		DirsIndexed:  2,
		FilesIndexed: 10,
		Phase:        "scan",
	})
	if err != nil {
		t.Fatalf("marshal progress: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	for field, want := range map[string]float64{
		"bytes_indexed": 512,
		"dirs_indexed":  2,
		"files_indexed": 10,
	} {
		if got, ok := fields[field].(float64); !ok || got != want {
			t.Errorf("%s = %#v, want %v", field, fields[field], want)
		}
	}
}
