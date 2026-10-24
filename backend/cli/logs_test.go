package main

import "testing"

func TestParseLogsArgsIndexer(t *testing.T) {
	mode, lines := parseLogsArgs([]string{"indexer", "25"})
	if mode != "indexer" || lines != 25 {
		t.Fatalf("parseLogsArgs = %q, %d; want indexer, 25", mode, lines)
	}
}
