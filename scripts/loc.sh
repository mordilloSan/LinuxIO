#!/usr/bin/env bash
# Count handwritten source lines per language (find + wc + awk, no cloc).
# Usage: loc.sh ROOT EXTS_CSV EXCLUDE_GENERATED(0|1) PRUNED_REL_DIR
set -u

root="$1" exts_csv="$2" exclude_gen="$3" pruned_rel_dir="$4"

if [ ! -d "$root" ]; then
	echo "(skipped: $root does not exist)"
	exit 0
fi

find_ext_args=()
IFS=',' read -ra exts <<<"$exts_csv"
for ext in "${exts[@]}"; do
	[ "${#find_ext_args[@]}" -eq 0 ] || find_ext_args+=(-o)
	find_ext_args+=(-name "*.$ext")
done

find "$root" \
	\( -type d \( -name node_modules \
	-o -path "*/$pruned_rel_dir" \
	-o -path "*/$pruned_rel_dir/*" \) -prune \) \
	-o -type f \( "${find_ext_args[@]}" \) -print0 |
	{
		if [ "$exclude_gen" = "1" ]; then
			grep -zvE '(routeTree\.gen\.ts|\.min\.js)$'
		else
			cat
		fi
	} |
	xargs -0 -r wc -l 2>/dev/null |
	awk '
        function basename_ext(p,   n, parts) {
          n = split(p, parts, ".");
          return (n > 1 ? parts[n] : "");
        }
        $1 ~ /^[0-9]+$/ {
          count = $1;
          match($0, /^[ \t]*[0-9]+[ \t]+/);
          path = substr($0, RSTART+RLENGTH);
          if (path == "total") next;
          ext = basename_ext(path);
          if (ext == "") next;
          lines[ext] += count; files[ext] += 1;
          total += count; total_files += 1;
        }
        END {
          if (total_files == 0) { print "(no files matched)"; exit }
          printf "%-10s %10s %10s\n", "Language", "Files", "Lines";
          printf "%-10s %10s %10s\n", "--------", "-----", "-----";
          for (e in lines) printf "%-10s %10d %10d\n", e, files[e], lines[e];
          printf "%-10s %10s %10s\n", "--------", "-----", "-----";
          printf "%-10s %10d %10d\n", "TOTAL", total_files, total;
        }'
