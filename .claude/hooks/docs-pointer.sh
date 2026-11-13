#!/usr/bin/env bash
# Claude Code hook. PostToolUse on Read, Edit and Write: the first time an agent opens a
# file that a doc covers (docs/README.md, Code column), tell it which doc. Each doc is named
# once per agent per session. PostCompact forgets what was said, so it is said again.
set -euo pipefail

root=${CLAUDE_PROJECT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
IFS=$'\t' read -r event session agent file < <(jq -r '[.hook_event_name // "-", .session_id // "-", .agent_id // "main", .tool_input.file_path // ""] | @tsv')
state_dir=${XDG_RUNTIME_DIR:-/tmp}/linuxio-docs-pointer
told=$state_dir/$session-$agent

if [[ $event == PostCompact ]]; then
	rm -f "$state_dir/$session-"*
	exit 0
fi

[[ $file == "$root"/* ]] || exit 0
file=${file#"$root"/}
# An agent in a worktree edits the same repo paths under .claude/worktrees/<name>/.
file=${file#.claude/worktrees/*/}
mkdir -p "$state_dir"
touch "$told"
# A doc the agent has opened itself needs no pointer.
if [[ $file == docs/* ]]; then
	printf '%s\n' "$file" >>"$told"
	exit 0
fi

docs=$("$root/scripts/docs-for.sh" "$file" | grep -vxFf "$told" || true)
[[ -n $docs ]] || exit 0
printf '%s\n' "$docs" >>"$told"
jq -n --arg file "$file" --arg docs "${docs//$'\n'/, }" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: "Docs covering \($file): \($docs). Read them before changing behaviour there, and update them in the same change when what they describe changes."}}'
