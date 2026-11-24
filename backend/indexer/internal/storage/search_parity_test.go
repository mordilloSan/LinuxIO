package storage

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/searchquery"
)

// A search inside a folder reads it on disk and matches names with
// searchquery.Matcher; what it finds must be what the index finds for the
// same names.

var parityNames = []string{
	"RELATÓRIO.odt", "relatório.txt", "Relatório final",
	"Report_2024.pdf", "report-final.PDF", "annual.pdf",
	"100%-done", "1000-done", "a_b.txt", "axb.txt", `back\slash`,
	"has two words", "two-words",
	"ÇÃO.md", "ção.md", "ΑΘΗΝΑ.txt", "αθηνα.txt", "ОТЧЁТ.doc", "отчёт.doc",
	"ΑΘΗΝΑΣ.txt", "µ-meter.txt", "μ-meter.txt", "ſtrange.txt", "strange.txt",
}

var parityQueries = []string{
	"relatório", "RELATÓRIO", "Relat", "report", "REPORT", "pdf",
	"case:exact Report", "case:exact relatório", "case:exact ÇÃ", "case:exact a_",
	"100%", "0%-", "a_b", "_b.", `k\s`, `"two words"`, `"report"`,
	"ção", "ÇÃO", "αθη", "ΑΘΗ", "отч", "ОТЧЁТ",
	"αθηνας", "ΑΘΗΝΑΣ", "µ-meter", "μ-meter", "Μ-METER", "strange", "ſtrange", "STRANGE",
	"foo|report", "|||report", "zzz|ΑΘΗ|txt",
	"ab|xyz", "a_|zzz", "0%|qq", "Pd|nope", "PDF|ab",
}

func seedParityNames(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	now := time.Now()
	seed := []indexing.IndexEntry{
		{RelativePath: "/", Name: "/", ModTime: now, Type: "directory"},
		{RelativePath: "/p", Name: "p", ModTime: now, Type: "directory"},
	}
	for _, name := range parityNames {
		seed = append(seed, indexing.IndexEntry{RelativePath: "/p/" + name, Name: name, Size: 1, ModTime: now, Type: "file"})
	}
	for _, e := range seed {
		if _, err := UpdateEntry(ctx, db, indexID, e); err != nil {
			t.Fatalf("seed %s: %v", e.RelativePath, err)
		}
	}
	return NewStoreWithDB(db, dbPath, nil), ctx
}

func indexMatches(t *testing.T, ctx context.Context, store *Store, query string) []string {
	t.Helper()
	results, err := store.SearchEntriesUnder(ctx, query, "/p", 100)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	var names []string
	for _, r := range results {
		if r.Path != "/p" {
			names = append(names, r.Name)
		}
	}
	slices.Sort(names)
	return names
}

func isASCII(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return r > unicode.MaxASCII })
}

// meantToAgree reports whether the index and the matcher are meant to agree
// on name for opts: always with case:exact and on the trigram path (every
// term of three runes or more); on the LIKE fallback, which folds ASCII only,
// for ASCII names and terms.
func meantToAgree(opts searchquery.Options, name string) bool {
	if opts.CaseSensitive {
		return true
	}
	short, ascii := false, true
	for _, term := range opts.Terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		short = short || utf8.RuneCountInString(term) < ftsMinTermRunes
		ascii = ascii && isASCII(term)
	}
	return !short || (ascii && isASCII(name))
}

func TestTheMatcherFindsWhatTheIndexFinds(t *testing.T) {
	skipWithoutFTS(t)
	store, ctx := seedParityNames(t)
	for _, query := range parityQueries {
		opts := searchquery.Parse(query)
		matcher := opts.Matcher()
		found := indexMatches(t, ctx, store, query)
		for _, name := range parityNames {
			if !meantToAgree(opts, name) {
				continue
			}
			byIndex := slices.Contains(found, name)
			if byMatcher := matcher.Match(name); byMatcher != byIndex {
				t.Errorf("query %q on %q: the index matched %v, the matcher %v", query, name, byIndex, byMatcher)
			}
		}
	}
}

// The known difference, pinned so that nobody makes the two agree by
// accident: with a term of one or two runes the index's LIKE fallback folds
// ASCII only, and the matcher folds Unicode, so a search that reads the disk
// finds a name the index misses. See docs/indexer.md, "Known limits".
func TestTheIndexFoldsASCIIOnlyForAShortTerm(t *testing.T) {
	skipWithoutFTS(t)
	store, ctx := seedParityNames(t)
	const query = "ó|xyz"
	if found := indexMatches(t, ctx, store, query); slices.Contains(found, "RELATÓRIO.odt") {
		t.Fatalf("the index's fallback found RELATÓRIO.odt for %q: it now folds Unicode, update the matcher's doc and docs/indexer.md", query)
	}
	if !searchquery.Parse(query).Matcher().Match("RELATÓRIO.odt") {
		t.Fatalf("the matcher does not fold Unicode for %q", query)
	}
}
