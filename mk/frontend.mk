# Frontend dependencies, lint, type-check, unit and browser tests, and builds.
# Quality-gate lanes that combine these live in quality.mk.

frontend_placeholder_files := \
	$(backend_frontend_dir)/.vite/manifest.json \
	$(backend_frontend_dir)/manifest.json \
	$(backend_frontend_dir)/favicon-1.png \
	$(backend_frontend_dir)/assets/index-mock.js

##@ Toolchain setup|green
setup: $(frontend_install_stamp) ## Install frontend dependencies (npm i)
	@:

$(frontend_install_stamp): $(frontend_dir)/package.json $(frontend_dir)/package-lock.json
	@echo ""
	@echo "📦 Installing frontend dependencies..."
	@cd "$(frontend_dir)" && npm install --silent
	@test -f "$@"
	@echo "✅ Frontend dependencies installed!"

update-deps: ensure-node ## Choose frontend upgrades, then install, audit and fix if needed
	@echo ""
	@echo "📦 Select frontend dependency upgrades"
	@"$(scripts_dir)/update-deps.sh" "$(frontend_dir)"

# CI installs are immutable and linting is read-only. Keep `setup`/`lint` as
# the developer workflow: those targets intentionally use npm install and
# auto-fixing lint tools so local changes can be repaired in place.
ci-frontend-deps: ## Install frontend dependencies from the lockfile (npm ci)
	@command -v npm >/dev/null 2>&1 || { echo "❌ npm is required (use actions/setup-node in CI)." >&2; exit 1; }
	@echo "📦 Installing frontend dependencies from the lockfile..."
	@cd "$(frontend_dir)" && npm ci --no-audit --no-fund

##@ Quality checks|green
lint: ensure-node setup ## Run OXLint + Oxfmt (frontend)
	@$(MAKE) --no-print-directory lint-only

lint-only: ## Run frontend lint without setup prerequisites
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔎 Running Oxlint + Oxfmt (auto-fix)...$(COLOR_RESET)"
	@"$(scripts_dir)/lint-frontend.sh" fix "$(frontend_dir)"

lint-ci: ci-frontend-deps ## Install locked frontend deps and run read-only lint
	@$(MAKE) --no-print-directory lint-ci-only

lint-ci-only:
	@"$(scripts_dir)/lint-frontend.sh" check "$(frontend_dir)"

tsc: ensure-node setup ## Type-check with TypeScript (frontend)
	@$(MAKE) --no-print-directory tsc-only

tsc-only: ## Run TypeScript checks without setup prerequisites
	@$(PRINTC) "$(COLOR_CYAN)🔎 Running TypeScript type checks...$(COLOR_RESET)"
	@cd "$(frontend_dir)" && ./node_modules/.bin/tsc && $(PRINTC) "$(COLOR_GREEN)✅ TypeScript checks passed!$(COLOR_RESET)"

tsc-ci: ci-frontend-deps ## Install locked frontend deps and run TypeScript checks
	@$(MAKE) --no-print-directory tsc-only

test-frontend: ensure-node setup ## Run frontend unit tests only
	@$(MAKE) --no-print-directory test-frontend-only

test-frontend-only: ## Run frontend unit tests without setup prerequisites
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running frontend unit tests...$(COLOR_RESET)"
	@"$(scripts_dir)/test-frontend.sh" "$(frontend_dir)" "$(setsid_cmd)"

test-frontend-ci: ci-frontend-deps ## Install locked frontend deps and run frontend tests
	@$(MAKE) --no-print-directory test-frontend-only

setup-frontend-browser: ensure-node setup ## Install Playwright Chromium
	@echo "🌐 Installing Playwright Chromium..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/playwright install chromium
	@echo "✅ Playwright Chromium installed!"

test-frontend-browser: ensure-node setup ## Build frontend + run router browser tests
	@set -e
	@echo "🏗️  Building the production frontend for chunk-boundary checks..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/vite build --config config/vite.config.ts --configLoader native
	@echo "🌐 Running frontend browser tests..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/playwright test --config config/playwright.config.ts $(PLAYWRIGHT_ARGS)
	@echo "✅ Frontend browser tests passed!"

# Rewrites the screenshot baselines src/test/browser/styling-gallery.spec.ts
# compares against. Run after a deliberate visual change, then review the PNG
# diff in git; the gallery does not need the production build.
update-frontend-screenshots: ensure-node setup ## Rewrite the browser screenshot baselines
	@echo "📸 Rewriting frontend screenshot baselines..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/playwright test --config config/playwright.config.ts --update-snapshots=all src/test/browser/styling-gallery.spec.ts
	@echo "✅ Screenshot baselines updated!"

bundle-metrics: ## Report frontend bundle sizes after a Vite build (informational)
	@echo ""
	@echo "📊 Reporting frontend bundle metrics..."
	@cd "$(frontend_dir)" && npm run bundle:metrics

analyze: ensure-node setup ## Build frontend with bundle analysis enabled
	@echo ""
	@echo "🔬 Building frontend bundle analysis..."
	@cd "$(frontend_dir)" && npm run analyze && echo "✅ Frontend analysis built successfully!"

##@ Development|yellow
dev-prep: $(frontend_placeholder_files) ## Create placeholder frontend assets for dev server

$(frontend_placeholder_files):
	@mkdir -p "$(@D)"
	@touch "$@"

##@ Build|yellow
build-vite: ## Build frontend static assets (Vite)
	@echo ""
	@echo "🏗️  Building frontend..."
	@cd "$(frontend_dir)" && npm run build && echo "✅ Frontend built successfully!"
