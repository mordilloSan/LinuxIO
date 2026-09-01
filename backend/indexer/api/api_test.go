package api

import "testing"

func TestSearchQueryAllowed(t *testing.T) {
	for _, test := range []struct {
		query string
		want  bool
	}{
		{query: "ab"},
		{query: "abc", want: true},
		{query: "case:exact ab"},
		{query: "case:exact Abc", want: true},
		{query: "ééé", want: true},
	} {
		if got := SearchQueryAllowed(test.query); got != test.want {
			t.Errorf("SearchQueryAllowed(%q) = %t, want %t", test.query, got, test.want)
		}
	}
}
