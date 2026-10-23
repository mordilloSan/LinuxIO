#!/usr/bin/env bash
# Interactively choose frontend dependency upgrades, then install, audit and fix.
# Usage: update-deps.sh FRONTEND_DIR
set -euo pipefail

test -t 0 && test -t 1 || {
	echo "Run make update-deps in an interactive terminal." >&2
	exit 1
}
cd "$1"
echo ""
echo "🔎 Current outdated packages:"
npm outdated || true
echo ""
echo "⬆️  Choose packages with Space; press Enter to apply."
before="$(sha256sum package.json)"
npx --yes npm-check-updates --interactive --interactiveSelect none --install never --target greatest --pre 0
if [ "$before" = "$(sha256sum package.json)" ]; then
	echo "No dependency upgrades selected."
	exit 0
fi
echo ""
echo "🔄 Refreshing lockfile + node_modules (npm install)..."
npm install --no-audit --no-fund
echo ""
echo "🛡️  Checking for npm vulnerabilities..."
if ! npm audit --audit-level=low; then
	echo "🛡️  Applying available npm audit fixes..."
	if ! npm audit fix --no-fund --audit-level=low; then
		echo "⚠️  npm audit fix did not resolve all vulnerabilities or could not complete; review the report below." >&2
		npm audit || true
	fi
fi
echo ""
echo "🔎 Remaining outdated after update:"
npm outdated || true
echo ""
echo "✅ Selected frontend dependencies updated!"
