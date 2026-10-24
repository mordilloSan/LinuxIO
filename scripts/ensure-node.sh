#!/usr/bin/env bash
# Install and activate Node.js through nvm. Run as `bash -l` so the login
# environment is available, as the Makefile does.
# Usage: ensure-node.sh NODE_VERSION NVM_VERSION NVM_DIR
node_version="$1" nvm_version="$2" NVM_DIR="$3"
export NVM_DIR

if [ ! -d "$NVM_DIR" ]; then
	curl -o- "https://raw.githubusercontent.com/nvm-sh/nvm/v${nvm_version}/install.sh" | bash
fi

# shellcheck source=/dev/null
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
nvm install "$node_version" >/dev/null || true
nvm alias default "$node_version"
curr="$(nvm version "$node_version")"
mkdir -p "$NVM_DIR/versions/node"
ln -snf "$NVM_DIR/versions/node/$curr" "$NVM_DIR/versions/node/current"
hash -r
echo "   Node path: $(command -v node)"
echo "   Node version: $(node -v)"
echo "   NPM version:  $(npm -v)"
echo "   NPX version:  $(npx -v)"
