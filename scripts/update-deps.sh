#!/usr/bin/env bash
# Interactively choose frontend dependency upgrades, then install, audit and fix,
# and print a suggested commit message for the result.
# Usage: update-deps.sh FRONTEND_DIR
set -euo pipefail

test -t 0 && test -t 1 || {
	echo "Run make update-deps in an interactive terminal." >&2
	exit 1
}

# Print a Conventional Commits message for the direct dependency changes
# between two package.json files.
# Usage: commit_message OLD_PACKAGE_JSON NEW_PACKAGE_JSON AUDIT_FIXED
commit_message() {
	node - "$@" <<'EOF'
const { readFileSync } = require("node:fs");
const [oldPath, newPath, auditFixed] = process.argv.slice(2);
const read = (path) => JSON.parse(readFileSync(path, "utf8"));
const groups = ["dependencies", "devDependencies", "optionalDependencies", "peerDependencies"];
const versions = (pkg) => Object.assign({}, ...groups.map((group) => pkg[group] ?? {}));
const before = versions(read(oldPath));
const after = versions(read(newPath));
const bare = (range) => range.replace(/^[\^~>=<\s]+/, "");
const lines = [];
for (const name of Object.keys({ ...before, ...after }).sort()) {
	if (before[name] === after[name]) continue;
	if (after[name] === undefined) lines.push(`remove dependency ${name}`);
	else if (before[name] === undefined) lines.push(`add dependency ${name} v${bare(after[name])}`);
	else lines.push(`update dependency ${name} from v${bare(before[name])} to v${bare(after[name])}`);
}
const single = lines.length === 1 ? `chore(deps): ${lines[0].replace(/ from v\S+/, "")}` : "";
const subject =
	single && single.length <= 72
		? single
		: `chore(deps): update ${lines.length} frontend ${lines.length === 1 ? "dependency" : "dependencies"}`;
const body = subject === single ? [] : lines.map((line) => `- ${line}`);
const paragraphs = [subject];
if (body.length > 0) paragraphs.push(body.join("\n"));
if (auditFixed === "1") paragraphs.push("Apply the available npm audit fixes to the lockfile.");
console.log(paragraphs.join("\n\n"));
EOF
}

cd "$1"
before="$(mktemp)"
trap 'rm -f "$before"' EXIT
cp package.json "$before"
echo ""
echo "🔎 Current outdated packages:"
npm outdated || true
echo ""
echo "⬆️  Choose packages with Space; press Enter to apply."
npx --yes npm-check-updates --interactive --interactiveSelect none --install never --target greatest --pre 0
if cmp -s "$before" package.json; then
	echo "No dependency upgrades selected."
	exit 0
fi
echo ""
echo "🔄 Refreshing lockfile + node_modules (npm install)..."
npm install --no-audit --no-fund
echo ""
echo "🛡️  Checking for npm vulnerabilities..."
audit_fixed=0
if ! npm audit --audit-level=low; then
	echo "🛡️  Applying available npm audit fixes..."
	lock_before="$(sha256sum package-lock.json)"
	if ! npm audit fix --no-fund --audit-level=low; then
		echo "⚠️  npm audit fix did not resolve all vulnerabilities or could not complete; review the report below." >&2
		npm audit || true
		echo "⚠️  npm audit fix has already run; what is listed above needs a manual upgrade or has no fix yet." >&2
	fi
	if [ "$lock_before" != "$(sha256sum package-lock.json)" ]; then
		audit_fixed=1
	fi
fi
echo ""
echo "🔎 Remaining outdated after update:"
npm outdated || true
echo ""
echo "✅ Selected frontend dependencies updated!"
echo ""
echo "📝 Suggested commit message:"
echo ""
commit_message "$before" package.json "$audit_fixed"
