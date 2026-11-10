#!/usr/bin/env bash
# Print a Conventional Commits message for the direct dependency changes
# between two go.mod files.
# Usage: go-deps-commit-message.sh OLD_GO_MOD NEW_GO_MOD
set -euo pipefail

# Emit "name version" for the go directive, the toolchain and every direct
# requirement, sorted by name.
versions() {
	awk '
		$1 == "go" && NF == 2 { print "Go", $2; next }
		$1 == "toolchain" && NF == 2 { print "toolchain", $2; next }
		/\/\/ indirect/ { next }
		$1 == "require" && $2 == "(" { block = 1; next }
		block && $1 == ")" { block = 0; next }
		$1 == "require" && NF >= 3 { print $2, $3; next }
		block && NF >= 2 { print $1, $2 }
	' "$1" | LC_ALL=C sort
}

describe() {
	case "$1" in
	Go | toolchain) echo "$1" ;;
	*) echo "module $1" ;;
	esac
}

lines=()
while read -r name old new; do
	[ "$old" = "$new" ] && continue
	subject="$(describe "$name")"
	if [ "$new" = "-" ]; then
		lines+=("remove $subject")
	elif [ "$old" = "-" ]; then
		lines+=("add $subject $new")
	else
		lines+=("update $subject from $old to $new")
	fi
done < <(LC_ALL=C join -a 1 -a 2 -e - -o 0,1.2,2.2 <(versions "$1") <(versions "$2"))

count="${#lines[@]}"
if [ "$count" -eq 0 ]; then
	echo "chore(deps): update indirect Go modules"
	exit 0
fi
single="chore(deps): $(sed -E 's/ from [^ ]+ to / to /' <<<"${lines[0]}")"
if [ "$count" -eq 1 ] && [ "${#single}" -le 72 ]; then
	echo "$single"
	exit 0
fi
if [ "$count" -eq 1 ]; then
	echo "chore(deps): update 1 Go dependency"
else
	echo "chore(deps): update $count Go dependencies"
fi
echo ""
printf -- '- %s\n' "${lines[@]}"
