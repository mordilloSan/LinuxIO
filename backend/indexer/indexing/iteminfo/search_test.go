package iteminfo

import (
	"testing"
)

func TestParseSearch(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		caseSensitive bool
		expectedTerms []string
	}{
		{
			name:          "simple search",
			input:         "test",
			caseSensitive: false,
			expectedTerms: []string{"test"},
		},
		{
			name:          "case sensitive search",
			input:         "case:exact Test",
			caseSensitive: true,
			expectedTerms: []string{"Test"},
		},
		{
			name:          "quoted exact phrase",
			input:         "\"exact phrase\"",
			caseSensitive: false,
			expectedTerms: []string{"exact phrase"},
		},
		{
			name:          "lone quote character is a literal term",
			input:         "\"",
			caseSensitive: false,
			expectedTerms: []string{"\""},
		},
		{
			name:          "multiple terms with OR",
			input:         "term1|term2|term3",
			caseSensitive: false,
			expectedTerms: []string{"term1", "term2", "term3"},
		},
		{
			name:          "case sensitive with OR",
			input:         "case:exact Term1|Term2",
			caseSensitive: true,
			expectedTerms: []string{"Term1", "Term2"},
		},
		{
			name:          "empty string",
			input:         "",
			caseSensitive: false,
			expectedTerms: []string{},
		},
		{
			name:          "term count capped at MaxSearchTerms",
			input:         "t1|t2|t3|t4|t5|t6|t7|t8|t9|t10|t11|t12",
			caseSensitive: false,
			expectedTerms: []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10"},
		},
		{
			name:          "whitespace only",
			input:         "   ",
			caseSensitive: false,
			expectedTerms: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSearch(tt.input)

			if result.CaseSensitive != tt.caseSensitive {
				t.Errorf("Expected CaseSensitive=%v, got %v", tt.caseSensitive, result.CaseSensitive)
			}

			if len(result.Terms) != len(tt.expectedTerms) {
				t.Errorf("Expected %d terms, got %d", len(tt.expectedTerms), len(result.Terms))
				return
			}

			for i, term := range tt.expectedTerms {
				if result.Terms[i] != term {
					t.Errorf("Expected term[%d]=%s, got %s", i, term, result.Terms[i])
				}
			}
		})
	}
}
