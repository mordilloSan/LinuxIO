#!/usr/bin/env bash
# Verify that relative markdown links and repo paths named in the docs exist.
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
exit "$fail"
