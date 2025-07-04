-include .env
-include secret.env

GO_VERSION      ?= 1.22.2
GO_INSTALL_DIR := $(HOME)/.go
NVM_SETUP = export NVM_DIR="$$HOME/.nvm"; . "$$NVM_DIR/nvm.sh"
GO_BIN := $(shell which go)
AIR_BIN := $(shell which air)
BRIDGE_BIN := /usr/lib/linuxio/linuxio-bridge

# Colors
COLOR_RESET  := \033[0m
COLOR_BLUE   := \033[1;34m
COLOR_GREEN  := \033[1;32m
COLOR_YELLOW := \033[1;33m
COLOR_CYAN   := \033[1;36m
COLOR_RED    := \033[1;31m

default: help

define check_var
	@if [ -z "$($1)" ]; then echo "❌ $1 not set. Please edit the file ".env""; exit 1; fi
endef

define check_var_sudo
	@if [ -z "$($1)" ]; then echo "❌ $1 not set. Please edit the file "secret.env""; exit 1; fi
endef

check-env:
	@echo ""
	@echo "🔍 Checking .env setup..."
	$(call check_var,SERVER_PORT)
	$(call check_var,VITE_DEV_PORT)
	$(call check_var,GO_VERSION)
	$(call check_var,NODE_VERSION)
	$(call check_var_sudo,SUDO_PASSWORD)
	@echo "✅ Environment looks good!"

ensure-node: check-env
	@echo ""
	@echo "📦 Ensuring Node.js $(NODE_VERSION) is available..."
	@if [ ! -d "$$HOME/.nvm" ]; then \
		curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.2/install.sh | bash; \
	fi
	@bash -c '\
	$(NVM_SETUP); \
		nvm install $(NODE_VERSION) > /dev/null; \
		nvm use $(NODE_VERSION) > /dev/null; \
		echo "✔ Node version: $$(node -v)"; \
		echo "✔ NPM version: $$(npm -v)"; \
		echo "✔ NPX version: $$(npx -v)"; \
	'
	@echo "✅ Node.js environment ready!"

ensure-go: check-env
	@echo ""
	@echo "📦 Ensuring Go is available..."
	@if ! command -v go >/dev/null 2>&1; then \
		echo "⬇ Installing Go (no sudo)..."; \
		curl -LO https://go.dev/dl/go$(GO_VERSION).linux-amd64.tar.gz; \
		rm -rf $(GO_INSTALL_DIR); \
		mkdir -p $(GO_INSTALL_DIR); \
		tar -C $(GO_INSTALL_DIR) -xzf go$(GO_VERSION).linux-amd64.tar.gz --strip-components=1; \
		rm go$(GO_VERSION).linux-amd64.tar.gz; \
		if ! grep -q 'export PATH=$(GO_INSTALL_DIR)/bin' $$HOME/.bashrc; then \
			echo 'export PATH=$(GO_INSTALL_DIR)/bin:$$PATH' >> $$HOME/.bashrc; \
		fi; \
		echo "✔ Go installed at $(GO_INSTALL_DIR)"; \
		echo "💡 Please run 'source ~/.bashrc' or restart your terminal to use Go globally."; \
	fi
	@bash -c 'export PATH=$(GO_INSTALL_DIR)/bin:$$PATH && go version'
	@echo "✅ Go is ready!"

ensure-golint:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "⬇ Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GO_INSTALL_DIR)/bin v1.58.2; \
	fi

setup: ensure-node ensure-go
	@echo ""
	@echo "📦 Installing frontend dependencies..."
	@bash -c '\
	$(NVM_SETUP); \
		cd react && npm install --silent; \
	'
	@echo "✅ Frontend dependencies installed!"

lint:
	@echo "🔍 Running ESLint..."
	@bash -c '$(NVM_SETUP); \
		cd react && \
		npx eslint src --ext .js,.jsx,.ts,.tsx --fix && echo "✅ React Linting Ok!"\
	'

tsc:
	@echo "🔍 Running TypeScript type checks..."
	@bash -c '$(NVM_SETUP); \
		cd react && \
		npx tsc && echo "✅ TypeScript Linting Ok!"\
	'

golint: ensure-golint
	@echo -n "🔍 Running golangci-lint... "; \
	cd go-backend && golangci-lint run --timeout 3m && echo "✅ Go Linting Ok!"

test: setup
	@echo ""
	@echo "📦 Running checks..."
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory tsc
	@$(MAKE) --no-print-directory golint

build-vite-dev: test
	@echo ""
	@echo "📦 Building frontend..."
	@bash -c '\
	$(NVM_SETUP); \
		cd react && \
		VITE_API_URL=http://localhost:$(SERVER_PORT) npx vite build && \
		echo "✅ Frontend built successfully!" \
	'

build-vite-prod: test
	@echo ""
	@echo "📦 Building frontend..."
	@bash -c '\
	$(NVM_SETUP); \
		cd react && \
		VITE_API_URL=/ npx vite build && \
		echo "✅ Frontend built successfully!" \
	'

build-backend: setup
	@echo ""
	@echo "📦 Building backend..."
	@cd go-backend/cmd/server && \
	go build \
	-ldflags "\
		-X 'main.version=$(VERSION)' \
		-X 'main.env=production' \
		-X 'main.buildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
	-o linuxio-webserver && \
	echo "✅ Backend built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: go-backend/server" && \
	echo "🔖 Version: $(VERSION)" && \
	echo "⏱ Build Time: $$(date -u +%Y-%m-%dT%H:%M:%SZ)" && \
	echo "📦 Size: $$(du -h linuxio-webserver | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 linuxio-webserver | awk '{ print $$1 }')"

build-bridge:
	@echo ""
	@echo "📦 Building backend bridge..."
	@echo "$(SUDO_PASSWORD)" | sudo -SE bash scripts/build-bridge.sh

dev-prep:
	@mkdir -p go-backend/frontend/assets
	@mkdir -p go-backend/frontend/.vite
	@touch go-backend/frontend/.vite/manifest.json
	@touch go-backend/frontend/manifest.json
	@touch go-backend/frontend/favicon-1.png
	@touch go-backend/frontend/assets/index-mock.js

dev: setup check-env dev-prep build-bridge
	@echo ""
	@echo "🚀 Starting dev mode (frontend + backend)..."
	@bash -c '\
	cd go-backend && \
	GO_ENV=development PATH="/usr/sbin:$(PATH)" $(AIR_BIN) \
	' &
	@sleep 1
	@bash -c '\
	$(NVM_SETUP); \
	cd react && VITE_API_URL=http://localhost:$(SERVER_PORT) npx vite --port $(VITE_DEV_PORT) \
	'

prod: check-env build-vite-prod build-bridge
	@cd go-backend/cmd/server && SERVER_PORT=$(SERVER_PORT) $(GO_BIN) run .

run: build-vite-prod build-backend build-bridge
	@sleep 1
	@echo "🚦 Starting backend server..."
	@cd go-backend/cmd/server && SERVER_PORT=$(SERVER_PORT) ./linuxio-webserver

clean: stop-bridge
	@rm -f go-backend/cmd/server/linuxio-webserver || true
	@rm -f go-backend/cmd/bridge/linuxio-bridge || true
	@rm -rf react/node_modules || true
	@rm -f react/package-lock.json || true
	@find go-backend/frontend -mindepth 1 -exec rm -rf {} + 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

help:
	@echo ""
	@echo "$(COLOR_BLUE)🛠️  Available commands:$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_GREEN)  make check-env       $(COLOR_RESET) Verify .env and required environment variables"
	@echo "$(COLOR_GREEN)  make setup           $(COLOR_RESET) Install Node.js, Go and frontend dependencies"
	@echo "$(COLOR_GREEN)  make lint            $(COLOR_RESET) Run ESLint linter on frontend"
	@echo "$(COLOR_GREEN)  make tsc             $(COLOR_RESET) Run TypeScript type checks on frontend"
	@echo "$(COLOR_GREEN)  make test            $(COLOR_RESET) Run ESLint + TypeScript type checks"
	@echo ""
	@echo "$(COLOR_YELLOW)  make dev             $(COLOR_RESET) Start frontend (Vite) and backend (Go) in dev mode (hot reload)"
	@echo "$(COLOR_YELLOW)  make prod            $(COLOR_RESET) Build production frontend, start backend (Go) in production mode"
	@echo "$(COLOR_YELLOW)  make run             $(COLOR_RESET) Full production build and start everything"
	@echo ""
	@echo "$(COLOR_CYAN)  make build-backend   $(COLOR_RESET) Build Go backend binary"
	@echo "$(COLOR_CYAN)  make build-bridge    $(COLOR_RESET) Build privileged helper bridge binary"
	@echo "$(COLOR_CYAN)  make build-vite-dev  $(COLOR_RESET) Build frontend static files (Vite) for development"
	@echo "$(COLOR_CYAN)  make build-vite-prod $(COLOR_RESET) Build frontend static files (Vite) for production"
	@echo ""
	@echo "$(COLOR_RED)  make clean           $(COLOR_RESET) Remove build artifacts and node_modules"
	@echo ""


.PHONY: all ensure-node ensure-go setup test dev dev-prep prod run build-vite-dev build-vite-prod build-backend build-bridge clean help lint tsc check-env stop-bridge
