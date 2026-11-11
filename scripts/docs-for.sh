#!/usr/bin/env bash
# List the docs that cover a path, from the Code column of docs/README.md.
#   docs-for.sh PATH...    docs covering those files or directories (repo-relative or absolute)
#   docs-for.sh --changed  the same for the files changed in the working tree, then the changed
#                          directories that only general docs cover, and those no doc covers
#   docs-for.sh --rows     every "doc<TAB>path" pair of the index; the path is "-" for a doc
#                          that covers no code and empty for a row without a Code cell
# A path in the index is general when two or more other docs cover code inside it, such as
# backend/bridge/handlers: Handler Patterns sets rules for every handler but describes none.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

rows() {
	awk -F'|' '/^\| *\[[^]]*\]\(\.\/[^)]+\.md\)/ {
		doc = $2
		sub(/^[^(]*\(\.\//, "docs/", doc)
		sub(/\).*$/, "", doc)
		n = split($3, part, "`")
		if (n < 3) {
			gsub(/ /, "", $3)
			print doc "\t" ($3 == "-" ? "-" : "")
		}
		for (i = 2; i < n; i += 2) print doc "\t" part[i]
	}' docs/README.md
}

# $1: index rows; stdout: the general paths among them.
general() {
	awk -F'\t' '$2 != "" && $2 != "-" { doc[NR] = $1; code[NR] = $2 }
		END {
			for (i in code) {
				split("", inside)
				n = 0
				for (j in code)
					if (doc[j] != doc[i] && index(code[j], code[i] "/") == 1 && !(doc[j] in inside)) {
						inside[doc[j]]
						n++
					}
				if (n >= 2) print code[i]
			}
		}' <<<"$1" | sort -u
}

# $1: index rows; stdin: repo paths, one per line; stdout: "doc<TAB>path" for every given
# path a doc covers. A directory also matches the docs covering something inside it, and a
# test file (x_test.go, x.test.ts) the docs covering the file it tests.
covering() {
	awk -F'\t' 'NR == FNR { if ($2 != "" && $2 != "-") { doc[NR] = $1; code[NR] = $2 }; next }
		{
			subject = $0
			sub(/_test\.go$/, ".go", subject)
			if (match(subject, /\.test\.[A-Za-z]+$/)) subject = substr(subject, 1, RSTART - 1) substr(subject, RSTART + 5)
			for (i in code)
				if ($0 == code[i] || subject == code[i] || index($0, code[i] "/") == 1 || index(code[i], $0 "/") == 1) print doc[i] "\t" $0
		}' <(printf '%s\n' "$1") - | sort -u
}

# stdin: changed files; stdout: their directories outside docs/, once each.
dirs() {
	grep -v '^docs/' | xargs -r -d '\n' dirname | sort -u
}

# $1: heading, $2: lines to list under it, if any.
list() {
	[[ -n $2 ]] || return 0
	printf '%s\n' "$1"
	printf '  %s\n' "${2//$'\n'/$'\n'  }"
}

case ${1:-} in
--rows) rows ;;
--changed)
	changed=$({
		git diff --name-only HEAD
		git ls-files --others --exclude-standard
	} | sort -u)
	if [[ -z $changed ]]; then
		echo "no changed files"
		exit 0
	fi
	index=$(rows)
	pairs=$(covering "$index" <<<"$changed")
	cut -f1 <<<"$pairs" | uniq -c | while read -r count doc; do
		[[ -n $doc ]] || continue
		state="not changed"
		! grep -qxF "$doc" <<<"$changed" || state=changed
		printf '%s (%s) covers %s changed files\n' "$doc" "$state" "$count"
	done
	specific=$(awk -F'\t' 'NR == FNR { general[$0]; next } !($2 in general)' <(general "$index") <(printf '%s\n' "$index"))
	covered=$(cut -f2 <<<"$pairs")
	specifically=$(covering "$specific" <<<"$changed" | cut -f2)
	list "only general docs cover changes in:" "$(grep -vxFf <(printf '%s\n' "$specifically") <<<"$covered" | dirs || true)"
	list "no doc covers changes in:" "$(grep -vxFf <(printf '%s\n' "$covered") <<<"$changed" | dirs || true)"
	;;
*)
	if (($# == 0)); then
		echo "usage: docs-for.sh PATH... | --changed | --rows" >&2
		exit 2
	fi
	for path; do
		path=${path#"$PWD"/}
		path=${path#./}
		printf '%s\n' "${path%/}"
	done | covering "$(rows)" | cut -f1 | sort -u
	;;
esac
