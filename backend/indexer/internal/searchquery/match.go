package searchquery

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Matcher matches names read from the disk the way the index's search
// matches its rows: the name holds one of the terms, as typed with
// case:exact, and otherwise with case folded rune by rune to one
// representative of the rune's Unicode simple case-folding orbit, so that
// "ς", "σ" and "Σ", or "µ" and "μ", or "ſ" and "s", are one rune.
//
// It is not the index's matching rune for rune, and nothing relies on that:
// a search whose disk read completed keeps every index result that is still
// on disk, whether the Matcher accepts its name or not. A table of names and
// queries checks that the two agree where they are meant to
// (search_parity_test.go in storage). They differ where the index folds
// less: its LIKE fallback, taken when a term has fewer than three runes,
// folds ASCII only, so there a Matcher finds names the index misses
// (TestTheIndexFoldsASCIIOnlyForAShortTerm).
type Matcher struct {
	caseSensitive bool
	terms         []string
}

// Matcher returns the matcher of o's terms, blank ones left out as the index
// leaves them out.
func (o Options) Matcher() Matcher {
	m := Matcher{caseSensitive: o.CaseSensitive}
	for _, term := range o.Terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if !m.caseSensitive {
			term = foldCase(term)
		}
		m.terms = append(m.terms, term)
	}
	return m
}

// Empty reports whether the matcher has no term, and so matches nothing.
func (m Matcher) Empty() bool {
	return len(m.terms) == 0
}

// Match reports whether name holds one of the terms.
func (m Matcher) Match(name string) bool {
	if m.Empty() {
		return false
	}
	if !m.caseSensitive {
		name = foldCase(name)
	}
	for _, term := range m.terms {
		if strings.Contains(name, term) {
			return true
		}
	}
	return false
}

func foldCase(s string) string {
	return strings.Map(foldRune, s)
}

// foldRune is the smallest rune of r's simple case-folding orbit.
func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'a' <= r && r <= 'z' {
			r -= 'a' - 'A'
		}
		return r
	}
	least := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		least = min(least, f)
	}
	return least
}
