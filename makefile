-include .env

GO_VERSION      = 1.22.2
NODE_VERSION    = 22
GO_INSTALL_DIR := $(HOME)/.go
NVM_SETUP = export NVM_DIR="$$HOME/.nvm"; . "$$NVM_DIR/nvm.sh"
GO_BIN := $(shell which go)

# Colors
COLOR_RESET  := \033[0m
COLOR_BLUE   := \033[1;34m
COLOR_GREEN  := \033[1;32m
COLOR_YELLOW := \033[1;33m
COLOR_CYAN   := \033[1;36m
COLOR_RED    := \033[1;31m

# Version auto-detection (from git tags)
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
GIT_COMMIT  := $(shell git rev-parse --short HEAD)
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

default: help

define check_var
	@if [ -z "$($1)" ]; then echo "❌ $1 not set. Please edit the file '.env'"; exit 1; fi
endef

check-env:
	@echo ""
	@echo "🔍 Checking .env setup..."
	$(call check_var,SERVER_PORT)
	$(call check_var,VITE_DEV_PORT)
	$(call check_var,GO_VERSION)
	$(call check_var,NODE_VERSION)
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
		cd frontend && npm install --silent; \
	'
	@echo "✅ Frontend dependencies installed!"

lint:
	@echo "🔍 Running ESLint..."
	@bash -c '$(NVM_SETUP); \
		cd frontend && \
		npx eslint src --ext .js,.jsx,.ts,.tsx --fix && echo "✅ frontend Linting Ok!"\
	'

tsc:
	@echo "🔍 Running TypeScript type checks..."
	@bash -c '$(NVM_SETUP); \
		cd frontend && \
		npx tsc && echo "✅ TypeScript Linting Ok!"\
	'

golint: ensure-golint
	@echo "🔍 Running gofmt -s -w ."
	@gofmt -s -w .
	@echo "🔍 Running golangci-lint..."
	@cd backend && golangci-lint run ./... --timeout 3m && echo "✅ Go Linting Ok!"

test: setup dev-prep
	@echo ""
	@echo "📦 Running checks..."
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory tsc
	@$(MAKE) --no-print-directory golint

build-vite: test
	@echo ""
	@echo "📦 Building frontend..."
	@bash -c '\
	$(NVM_SETUP); \
		cd frontend && \
		VITE_API_URL=/ npx vite build && \
		echo "✅ Frontend built successfully!" \
	'

build-backend: setup
	@echo ""
	@echo "📦 Building backend..."
	@cd backend && \
	go build \
	-ldflags "\
		-X 'backend/version.Version=$(GIT_VERSION)' \
		-X 'backend/version.CommitSHA=$(GIT_COMMIT)' \
		-X 'backend/version.BuildTime=$(BUILD_TIME)' \
		-X 'backend/version.Env=production'" \
	-o ../linuxio-webserver && \
	echo "✅ Backend built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-webserver" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo "🔐 Commit: $(GIT_COMMIT)" && \
	echo "⏱ Build Time: $(BUILD_TIME)" && \
	echo "📦 Size: $$(du -h ../linuxio-webserver | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio-webserver | awk '{ print $$1 }')"

build-bridge: setup
	@echo ""
	@echo "🔌 Building bridge..."
	@cd backend/cmd/bridge && \
	go build \
	-ldflags "\
		-X 'backend/version.Version=$(GIT_VERSION)' \
		-X 'backend/version.CommitSHA=$(GIT_COMMIT)' \
		-X 'backend/version.BuildTime=$(BUILD_TIME)' \
		-X 'backend/version.Env=production'" \
	-o ../../../linuxio-bridge && \
	echo "✅ Bridge built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-bridge" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo "🔐 Commit: $(GIT_COMMIT)" && \
	echo "⏱ Build Time: $(BUILD_TIME)" && \
	echo "📦 Size: $$(du -h ../../../linuxio-bridge | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../../../linuxio-bridge | awk '{ print $$1 }')"

dev-prep:
	@mkdir -p backend/cmd/server/frontend/assets
	@mkdir -p backend/cmd/server/frontend/.vite
	@touch backend/cmd/server/frontend/.vite/manifest.json
	@touch backend/cmd/server/frontend/manifest.json
	@touch backend/cmd/server/frontend/favicon-1.png
	@touch backend/cmd/server/frontend/assets/index-mock.js

dev: setup check-env dev-prep build-bridge
	@echo ""
	@echo "🚀 Starting dev mode (frontend + backend)..."
	@cd backend && GO_ENV=development go run . &
	@sleep 1
	@bash -c '\
	$(NVM_SETUP); \
	cd frontend && VITE_API_URL=http://localhost:$(SERVER_PORT) npx vite --port $(VITE_DEV_PORT) \
	'

build: check-env build-vite build-backend build-bridge

run:
	@SERVER_PORT=$(SERVER_PORT) ./linuxio-webserver

clean:
	@rm -f ./linuxio-webserver || true
	@rm -f ./linuxio-bridge || true
	@rm -rf frontend/node_modules || true
	@rm -f frontend/package-lock.json || true
	@find backend/cmd/server/frontend -mindepth 1 -exec rm -rf {} + 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

help:
	@echo ""
	@echo "$(COLOR_BLUE)🛠️  Available commands:$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_GREEN)  make check-env       $(COLOR_RESET) Verify .env and required environment variables"
	@echo "$(COLOR_GREEN)  make setup           $(COLOR_RESET) Install Node.js, Go and frontend dependencies"
	@echo "$(COLOR_GREEN)  make lint            $(COLOR_RESET) Run ESLint linter on frontend"
	@echo "$(COLOR_GREEN)  make tsc             $(COLOR_RESET) Run TypeScript type checks on frontend"
	@echo "$(COLOR_GREEN)  make golint          $(COLOR_RESET) Run golint linter on backend"
	@echo "$(COLOR_GREEN)  make test            $(COLOR_RESET) Run ESLint + TypeScript + golint checks"
	@echo ""
	@echo "$(COLOR_YELLOW)  make dev             $(COLOR_RESET) Start frontend (Vite) and backend (Go) in dev mode (no hot reload)"
	@echo "$(COLOR_YELLOW)  make build           $(COLOR_RESET) Build frontend, backend, and bridge for production"
	@echo "$(COLOR_YELLOW)  make run             $(COLOR_RESET) Run production backend server"
	@echo ""
	@echo "$(COLOR_CYAN)  make build-backend   $(COLOR_RESET) Build Go backend binary"
	@echo "$(COLOR_CYAN)  make build-bridge    $(COLOR_RESET) Build Go bridge binary"
	@echo "$(COLOR_CYAN)  make build-vite      $(COLOR_RESET) Build frontend static files (Vite) for production"
	@echo ""
	@echo "$(COLOR_RED)  make clean           $(COLOR_RESET) Remove build artifacts and node_modules"
	@echo ""

.PHONY: all ensure-node ensure-go setup test dev dev-prep build run build-vite build-backend build-bridge clean help lint tsc check-env golint
