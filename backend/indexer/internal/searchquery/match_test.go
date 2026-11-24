package searchquery

import "testing"

func TestMatcher(t *testing.T) {
	tests := []struct {
		query string
		name  string
		want  bool
	}{
		{"report", "annual-REPORT.pdf", true},
		{"report", "annual.pdf", false},
		{"relatório", "RELATÓRIO.odt", true},
		{"RELATÓRIO", "relatório.odt", true},
		{"ó|xyz", "RELATÓRIO.odt", true},
		{"case:exact Report", "report.txt", false},
		{"case:exact Report", "Report.txt", true},
		{"case:exact relatório", "RELATÓRIO.odt", false},
		{"100%", "100%-done", true},
		{"100%", "1000-done", false},
		{"a_b", "a_b.txt", true},
		{"a_b", "axb.txt", false},
		{`"two words"`, "has two words in it", true},
		{`"two words"`, "two-words", false},
		{"foo|bar|baz", "my-bar", true},
		{"foo||", "foo.txt", true},
		{"|||", "anything", false},
		{" | ", "anything", false},
	}
	for _, tt := range tests {
		if got := Parse(tt.query).Matcher().Match(tt.name); got != tt.want {
			t.Errorf("query %q on %q matched %v, want %v", tt.query, tt.name, got, tt.want)
		}
	}
}

func TestAMatcherWithoutTermsMatchesNothing(t *testing.T) {
	if !Parse("|||").Matcher().Empty() {
		t.Fatal("a query of blank terms is not empty")
	}
	if Parse("abc").Matcher().Empty() {
		t.Fatal("a query with a term is empty")
	}
}

func TestTheMatcherFoldsEveryCaseOfARune(t *testing.T) {
	for _, tt := range []struct{ query, name string }{
		{"αθηνας", "ΑΘΗΝΑΣ.txt"},
		{"ΑΘΗΝΑΣ", "αθηνας.txt"},
		{"µ-meter", "μ-meter.txt"},
		{"μ-meter", "µ-meter.txt"},
		{"strange", "ſtrange.txt"},
		{"ſtrange", "STRANGE.txt"},
		{"kelvin", "Kelvin"},
	} {
		if !Parse(tt.query).Matcher().Match(tt.name) {
			t.Errorf("query %q does not match %q", tt.query, tt.name)
		}
	}
}
