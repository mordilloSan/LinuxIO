#!/usr/bin/env bash
# Verify that relative markdown links and repo paths named in the docs exist, and
# that the index in docs/README.md has a row with existing code for every doc.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

fail=0
report() {
	printf '%s:%s: missing %s\n' "$1" "$2" "$3"
	fail=1
}

# Implementation plans name files that do not exist yet, so they are not checked.
mapfile -t files < <(find docs -name '*.md' -not -path 'docs/superpowers/plans/*' | sort && printf '%s\n' README.md AGENTS.md)

for f in "${files[@]}"; do
	dir=$(dirname "$f")
	# (a) relative markdown links: file:line:target
	while IFS=: read -r line target; do
		target=${target%%#*}
		[ -z "$target" ] && continue
		[ -e "$dir/$target" ] || report "$f" "$line" "$target"
	done < <(rg -no '\]\(([^)\s]+)\)' -r '$1' "$f" |
		rg -v ':(https?:|mailto:|#)' || true)
	# (b) repo path tokens: file:line:path
	while IFS=: read -r line token; do
		p=$(printf '%s' "$token" | sed -E 's/:[0-9]+(-[0-9]+)?$//; s/[.,;:)]+$//')
		case "$p" in *'*'* | *'<'* | *'>'* | *'{'* | *'$'*) continue ;; esac
		[ -e "$p" ] || report "$f" "$line" "$p"
	done < <(rg -no '(^|[^A-Za-z0-9_./-])(backend/|frontend/src/|docs/|mk/|scripts/|\.github/)[A-Za-z0-9_./@-]*(:[0-9]+(-[0-9]+)?)?' -r '$0' "$f" |
		sed -E 's/^([0-9]+):[^A-Za-z0-9_.]?/\1:/' || true)
done

# (c) the index maps code to docs: every doc in docs/ has a row in docs/README.md, and
# every row names code that exists, or "-" for a doc that covers none.
index=$(bash scripts/docs-for.sh --rows)
for f in docs/*.md; do
	[ "$f" = docs/README.md ] && continue
	grep -qF "$f"$'\t' <<<"$index" || report docs/README.md index "a row for $f"
done
while IFS=$'\t' read -r doc path; do
	case "$path" in
	-) ;;
	'') report docs/README.md index "the Code cell of $doc (use - for a doc that covers no code)" ;;
	*) [ -e "$path" ] || report docs/README.md index "$path, the code named for $doc" ;;
	esac
done <<<"$index"
exit "$fail"
