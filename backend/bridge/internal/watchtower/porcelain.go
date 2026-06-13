package watchtower

import (
	"regexp"
	"strings"
)

// porcelainLine matches one line of `--porcelain v1` output:
//
//	<name> (<image>): <State>[ Error: <message>]
var porcelainLine = regexp.MustCompile(`^(\S+) \((.*)\): ([A-Za-z]+)(?: Error: (.*))?$`)

const porcelainNoMatch = "no containers matched filter"

// ParsePorcelain converts Watchtower porcelain v1 output into Results.
// Unrecognized lines are skipped so stray log output cannot fail a run; an
// empty selection ("no containers matched filter") yields no results.
func ParsePorcelain(output string) []Result {
	var results []Result
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == porcelainNoMatch {
			continue
		}
		m := porcelainLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		results = append(results, Result{
			Name:  strings.TrimPrefix(m[1], "/"),
			Image: m[2],
			State: State(m[3]),
			Err:   strings.TrimSpace(m[4]),
		})
	}
	return results
}
