# Development targets and overrides

The repository Makefile is the supported entry point for setup, checks, tests,
builds, and packaging. Prefer focused targets when working on one area:

```sh
make check-frontend
make check-backend
make test-frontend-browser
```

Every validation target also has a `-quiet` form. It prints a short summary and
saves the complete output under `quiet_log_dir` (default:
`.cache/test-logs`):

```sh
make check-backend-quiet
make test-quiet quiet_failure_lines=80
```

Do not invoke `go test`, Vitest, TypeScript, linters, or formatters directly;
use the corresponding Make target.

## Prerequisites

- Go 1.27 or newer
- Node.js 24 or newer
- GNU Make

For Debian/Ubuntu, install the development libraries and optional analysis/test
tools with:

```sh
sudo apt install -y build-essential libpam0g-dev libsystemd-dev cppcheck \
  clang-tools clang-tidy bear libpam-wrapper libnss-wrapper libuid-wrapper
```

For Fedora/RHEL/CentOS, use:

```sh
sudo dnf install -y gcc pam-devel systemd-devel cppcheck clang-tools-extra \
  clang-tidy bear pam_wrapper nss_wrapper uid_wrapper
```

The static-analysis tools are only needed by `analyze-auth`. The wrapper
libraries are needed by `test-auth-pam`; that target skips when they are not
installed.

## Initial setup

```sh
git clone https://github.com/mordilloSan/LinuxIO
cd LinuxIO
make build
make localinstall
```

For frontend hot reload, start the backend through systemd and run `make dev`.
Vite serves on port `3000` by default and proxies backend requests to port
`8090`. Go/backend or bridge changes can be rebuilt with `make fastbuild` and
installed with `make localinstall`.

## Overrides

Override values on the command line (`make test GO_TEST_FLAGS=-count=1`) or in
the environment. The lowercase names used internally are implementation
details; the uppercase aliases below are kept for CI and existing scripts.

### Repository and artifact paths

| Variable | Default | Purpose |
| --- | --- | --- |
| `REPO_ROOT` | Makefile directory | Repository root. |
| `FRONTEND_DIR` | `$(REPO_ROOT)/frontend` | Frontend project directory. |
| `BACKEND_DIR` | Auto-detected `backend/` or repository root | Go module directory. |
| `CACHE_DIR` | `$(REPO_ROOT)/.cache` | Make caches, logs, and analysis artifacts. |
| `BIN_DIR` | `$(REPO_ROOT)` | Built binary output directory. |
| `GO_TOOLS_DIR` | `$(HOME)/.go` | Installed Go tool binaries. |
| `GO_TOOLCHAIN_VERSIONS_DIR` | `$(HOME)/.go-versions` | Managed Go installations. |
| `NVM_DIR` | `$(HOME)/.nvm` | Node Version Manager installation. |
| `VITE_DEV_LOG` | `$(FRONTEND_DIR)/.vite-dev.log` | Vite log file. |
| `VITE_DEV_PID` | `$(FRONTEND_DIR)/.vite-dev.pid` | Vite PID file. |
| `SCRIPT_SERVER_PID` | `.script-server.pid` | Development script-server PID file. |
| `quiet_log_dir` | `$(CACHE_DIR)/test-logs` | Full logs created by `*-quiet` targets. |

Paths are quoted by the recipes. Keep overrides confined to the repository or
an intentional user-local tool directory.

### Toolchains and commands

| Variable | Default | Purpose |
| --- | --- | --- |
| `GO_VERSION` | `go.mod` version | Go version installed by `ensure-go`. |
| `NODE_VERSION` | `frontend/package.json` engine | Node version installed by `ensure-node`. |
| `NVM_VERSION` | `0.40.2` | NVM installer release. |
| `GOOS` | `linux` | Go target operating system; LinuxIO validates this as `linux`. |
| `GOARCH` | Host architecture | Go target architecture (`amd64` or `arm64`). |
| `GOAMD64` | `v3` | amd64 microarchitecture level. |
| `GO_TOOLCHAIN` | `auto` | Go toolchain selection mode. |
| `GO_BIN` | Managed Go binary | Go executable; useful for CodeQL or system-Go integration. |
| `CC` | `cc` | C compiler for the authentication helper. |
| `sha256_cmd` | `sha256sum` | SHA-256 command; use `shasum -a 256` where required by a host. |
| `setsid_cmd` | `setsid` | Session launcher used to terminate complete validation process groups. |
| `GOLANGCI_LINT` | `$(GO_TOOLS_DIR)/bin/golangci-lint` | golangci-lint executable. |
| `GOLANGCI_LINT_VERSION` | `latest` | golangci-lint version installed by Make. |
| `MODERNIZE` | `$(GO_TOOLS_DIR)/bin/modernize` | modernize executable. |
| `MODERNIZE_VERSION` | `latest` | modernize version installed by Make. |
| `DEADCODE` | `$(GO_TOOLS_DIR)/bin/deadcode` | deadcode executable. |
| `DEADCODE_VERSION` | `latest` | deadcode version installed by Make. |
| `GOVULNCHECK` | `$(GO_TOOLS_DIR)/bin/govulncheck` | govulncheck executable. |
| `GOVULNCHECK_VERSION` | `latest` | govulncheck version installed by Make. |
| `GOLANGCI_LINT_OPTS` | `--modules-download-mode=mod` | Additional golangci-lint options. |

`SKIP_ENSURE_GO=1` skips managed Go installation and uses `GO_BIN` or the
system `go`. `CODEQL_ACTION_GO_BINARY` is honored automatically when present.

### Test, analysis, and build behavior

| Variable | Default | Purpose |
| --- | --- | --- |
| `GO_TEST_FLAGS` | empty | Extra flags for backend tests; `-count=1` disables Go test caching. |
| `VITEST_MAX_WORKERS` | `8` | Maximum Vitest workers; lower this on memory-constrained hosts. |
| `VITEST_FILE` | empty | Frontend-relative test/spec file for a focused Vitest run. |
| `VITEST_TEST_NAME` | empty | Vitest test-name regex, optionally combined with `VITEST_FILE`. |
| `GO_BUILD_EXTRA_ENV` | empty | Extra environment assignments for Go builds. |
| `GO_BUILD_TAGS` | empty | Additional Go build tags. |
| `DEADCODE_PARALLEL` | `auto` | Deadcode scheduling: `auto`, `0` (serial), or `1` (parallel). |
| `DEADCODE_CACHE` | `1` | Reuse content-addressed deadcode results; set `0` to rescan. |
| `LTO` | `1` | Enable link-time optimization for the C helper. |
| `STRIP` | `1` | Strip unneeded symbols from the C helper. |
| `WERROR` | `0` | Treat C compiler warnings as errors. |
| `VERBOSE` | `true` | Enable verbose frontend tooling output. |
| `quiet_failure_lines` | `40` | Failure-tail length for `*-quiet` targets; `0` suppresses the tail. |
| `LOC_INCLUDE_EXT` | `js,jsx,ts,tsx,css,scss,html,go` | Extensions counted by `make cloc`. |

### Development server inputs

| Variable | Default | Purpose |
| --- | --- | --- |
| `VITE_DEV_PORT` | `3000` | Vite development server port (`1`–`65535`). |
| `SCRIPT_SERVER_PORT` | `9999` | Packaging-script HTTP server port (`1`–`65535`). |
| `DEV_LOG_LINES` | `25` | Number of LinuxIO log lines shown by `make dev`. |

### Target-specific inputs

These are consumed by particular targets rather than global defaults:

- `BRIDGE_SHA256` supplies the bridge digest embedded by `build-backend`.
- `LINUXIO_CWRAP_LIBDIR` points `test-auth-pam` at wrapper libraries installed
  outside standard system library directories.
- `CFLAGS`, `LDFLAGS`, `GOOS`, and `GOARCH` remain standard toolchain
  overrides and are passed through where applicable.

## Release overrides

Private release targets are loaded from `release.mk` when that file is
available. Their supported inputs are:

| Variable | Purpose |
| --- | --- |
| `REPO` | Repository passed to GitHub CLI commands, for example `owner/name`. |
| `VERSION` | Release version consumed by `start-dev`/`open-pr`, such as `v1.2.3`. |
| `PR` | Optional pull-request number for `merge-release`. |

GitHub CLI authentication is provided by its normal credential mechanism; no
credentials are stored in Make variables.

## Target index

`make help` is the concise target index. It groups the supported targets as
follows:

- setup: `ensure-node`, `ensure-go`, `ensure-golint`, `ensure-modernize`,
  `ensure-deadcode`, `ensure-govulncheck`, `setup`, `update-deps`
- frontend checks: `lint`, `lint-only`, `tsc`, `tsc-only`, `test-frontend`,
  `test-frontend-only`, `lint-ci`, `tsc-ci`, `test-frontend-ci`,
  `check-frontend`, `setup-frontend-browser`, `test-frontend-browser`
- backend checks: `golint`, `golint-only`, `test-backend`, `deadcode`,
  `deadcode-only`, `check-backend`, `test-auth`, `test-auth-protocol`,
  `test-auth-pam`, `test-updater`, `test-docker-update-integration`,
  `check-c-build-deps`
- analysis: `bundle-metrics`, `compiler-coverage`, `analyze`, `analyze-auth`
- builds and development: `test`, `build`, `build-nocheck`, `fastbuild`,
  `build-vite`, `build-backend`, `build-bridge`, `build-auth`, `build-cli`,
  `build-docker-update`, `build-leak-profile`, `generate`, `dev-prep`, `dev`,
  `clean`, `cloc`
- installation: `localinstall`, `reinstall`, `uninstall`
- documentation: `help-overrides`

For release-helper smoke coverage, run `make test-release-automation`.

`_build-binaries` is an internal implementation target. It remains callable
for diagnostics but is not part of the normal user workflow.
