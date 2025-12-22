
## v0.5.2 — 2025-12-22

### 🚀 Features

* feat: - implemented a continous data connection, with ping/pong alive schema from the server. ([bf595b9](https://github.com/mordilloSan/LinuxIO/commit/bf595b9)) by @MordilloSan

### 🐛 Bug Fixes

* fix: - improved path inclusion on terminal fix: - improved react rerender performance on status bar and breadcrumbs ([483bcc1](https://github.com/mordilloSan/LinuxIO/commit/483bcc1)) by @MordilloSan
* fix: - Revert buggy ping/pong ([b50d628](https://github.com/mordilloSan/LinuxIO/commit/b50d628)) by @MordilloSan
* fix: - linting ([1a711cb](https://github.com/mordilloSan/LinuxIO/commit/1a711cb)) by @MordilloSan

### ♻️ Refactoring

* refactor: - SidebarNavList.tsx - Added React.memo wrapper - SidebarContext.tsx - Removed hovered state from context (was causing entire page to re-render) - Sidebar.tsx - Made hover state local, only triggers when sidebar is collapsed ([807745e](https://github.com/mordilloSan/LinuxIO/commit/807745e)) by @MordilloSan
* refactor: - linting and error checking ([fcfbc8b](https://github.com/mordilloSan/LinuxIO/commit/fcfbc8b)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.5.1...v0.5.2


## v0.5.1 — 2025-12-22

### 🚀 Features

* feat: -websocket and streaming reconnection and keepalive ([dd89a2c](https://github.com/mordilloSan/LinuxIO/commit/dd89a2c)) by @MordilloSan

### 🐛 Bug Fixes

* fix: - linting ([ff50f41](https://github.com/mordilloSan/LinuxIO/commit/ff50f41)) by @MordilloSan
* fix: - changelog display in PR improvement fix: - readme title position ([2670536](https://github.com/mordilloSan/LinuxIO/commit/2670536)) by @MordilloSan
* fix: - changelog build fix ([4448a85](https://github.com/mordilloSan/LinuxIO/commit/4448a85)) by @MordilloSan

### ♻️ Refactoring

* refactor: - old IPC code cleanup fix: - bridge now allows sudo ([224d340](https://github.com/mordilloSan/LinuxIO/commit/224d340)) by @MordilloSan
* refactor: - IPC code cleanup fix: - theming get bugfix ([a1c002d](https://github.com/mordilloSan/LinuxIO/commit/a1c002d)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.5.0...v0.5.1

# Changelog


## v0.5.0 — 2025-12-22

### 🐛 Bug Fixes

* fix: - Vite build process fix ([41deae0](https://github.com/mordilloSan/LinuxIO/commit/41deae0)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #51 from mordilloSan/dev/v0.5.0 ([ce0057f](https://github.com/mordilloSan/LinuxIO/commit/ce0057f)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.4.3...v0.5.0

## v0.4.3 — 2025-12-21

### 🚀 Features

* feat: add yamux binary streaming with persistent sessions ([6f28416](https://github.com/mordilloSan/LinuxIO/commit/6f28416)) by @MordilloSan
* feat: streaming for download and upload ([dac1948](https://github.com/mordilloSan/LinuxIO/commit/dac1948)) by @MordilloSan
* feat(filebrowser): migrate file transfers to yamux binary streams ([a252383](https://github.com/mordilloSan/LinuxIO/commit/a252383)) by @MordilloSan
* feat: - migrating api handlers to bridge streaming... ([2db29f9](https://github.com/mordilloSan/LinuxIO/commit/2db29f9)) by @MordilloSan

### 🐛 Bug Fixes

* fix: - frontend linting feat: - update IPC doc ([1e9c052](https://github.com/mordilloSan/LinuxIO/commit/1e9c052)) by @MordilloSan
* fix: - reworked download folder and compressing/extract works ([35f82b7](https://github.com/mordilloSan/LinuxIO/commit/35f82b7)) by @MordilloSan
* fix(streams): distinguish persistent vs ephemeral stream types ([a8d4f26](https://github.com/mordilloSan/LinuxIO/commit/a8d4f26)) by @MordilloSan
* fix: - bug in applying future update feat: implemented a crude version of notifications using toast messages... ([820e5e0](https://github.com/mordilloSan/LinuxIO/commit/820e5e0)) by @MordilloSan
* fix: - naming cleanup ([0190984](https://github.com/mordilloSan/LinuxIO/commit/0190984)) by @MordilloSan
* fix: - touchups ([67deb7d](https://github.com/mordilloSan/LinuxIO/commit/67deb7d)) by @MordilloSan
* fix: - CodeQL fix ([6a3ca73](https://github.com/mordilloSan/LinuxIO/commit/6a3ca73)) by @MordilloSan

### ♻️ Refactoring

* refactor: - cleanup dead code ([d2c0392](https://github.com/mordilloSan/LinuxIO/commit/d2c0392)) by @MordilloSan
* refactor: - frontend linting - notifications plan - TS fixes ([0f241de](https://github.com/mordilloSan/LinuxIO/commit/0f241de)) by @MordilloSan
* refactor: - migrating to streaming Fix: - react compiler linting ([add5034](https://github.com/mordilloSan/LinuxIO/commit/add5034)) by @MordilloSan
* refactor: - migrated all update code to the bridge - migration to streaming ([95a0f38](https://github.com/mordilloSan/LinuxIO/commit/95a0f38)) by @MordilloSan
* refactor: - migration to streaming ([36fd96d](https://github.com/mordilloSan/LinuxIO/commit/36fd96d)) by @MordilloSan
* refactor: - wireguard migration to streaming ([0edf961](https://github.com/mordilloSan/LinuxIO/commit/0edf961)) by @MordilloSan
* refactor: - router code cleanup ([aa04b12](https://github.com/mordilloSan/LinuxIO/commit/aa04b12)) by @MordilloSan
* refactor: - migrating to streaming ([14e8f4e](https://github.com/mordilloSan/LinuxIO/commit/14e8f4e)) by @MordilloSan
* refactor: full filebrowser streaming migration ([cb9cc40](https://github.com/mordilloSan/LinuxIO/commit/cb9cc40)) by @MordilloSan
* refactor: - code cleanup ([6f2ea1a](https://github.com/mordilloSan/LinuxIO/commit/6f2ea1a)) by @MordilloSan
* refactor: -docker terminal migration ([fa5a96d](https://github.com/mordilloSan/LinuxIO/commit/fa5a96d)) by @MordilloSan
* refactor: - legacy websocket removed. ([7cbb35f](https://github.com/mordilloSan/LinuxIO/commit/7cbb35f)) by @MordilloSan
* refactor: - remaining handlers mifrated ([7de36e4](https://github.com/mordilloSan/LinuxIO/commit/7de36e4)) by @MordilloSan
* refactor: -axios cleanup and logging improvement fix: - better server shutdown logging ([d0834de](https://github.com/mordilloSan/LinuxIO/commit/d0834de)) by @MordilloSan
* refactor: -todo update ([a830326](https://github.com/mordilloSan/LinuxIO/commit/a830326)) by @MordilloSan

### 🔄 Other Changes

* filebrowser: move transfers/compression to yamux streams ([de1d785](https://github.com/mordilloSan/LinuxIO/commit/de1d785)) by @MordilloSan
* -fix: bug in calculatin proper network usage ([27f2135](https://github.com/mordilloSan/LinuxIO/commit/27f2135)) by @MordilloSan
* reactor: - migration to streaming ([f62f9e3](https://github.com/mordilloSan/LinuxIO/commit/f62f9e3)) by @MordilloSan
* Merge pull request #50 from mordilloSan/dev/v0.4.3 ([49e207e](https://github.com/mordilloSan/LinuxIO/commit/49e207e)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.4.2...v0.4.3

## v0.4.2 — 2025-12-20

### 🚀 Features

* feat(security): add SHA256 hash validation for bridge binary ([0a8af0e](https://github.com/mordilloSan/LinuxIO/commit/0a8af0e)) by @MordilloSan

### 🐛 Bug Fixes

* fix: terminal lazy start and false update notifications in dev mode ([8fb2ede](https://github.com/mordilloSan/LinuxIO/commit/8fb2ede)) by @MordilloSan
* fix: terminal slow startup ([35f688c](https://github.com/mordilloSan/LinuxIO/commit/35f688c)) by @MordilloSan

### ♻️ Refactoring

* refactor: power action overlay, config paths, and build improvements ([991e66c](https://github.com/mordilloSan/LinuxIO/commit/991e66c)) by @MordilloSan

### 🔄 Other Changes

* fix - error checking linter update - frontend package update fix - TS in network graph ([f1034e6](https://github.com/mordilloSan/LinuxIO/commit/f1034e6)) by @MordilloSan
* Merge pull request #49 from mordilloSan/dev/v0.4.2 ([d4f0614](https://github.com/mordilloSan/LinuxIO/commit/d4f0614)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.4.1...v0.4.2

## v0.4.1 — 2025-12-20

### 🐛 Bug Fixes

* fix: D-Bus deadlock in reboot/poweroff, improve install docs and scripts ([9ebd82f](https://github.com/mordilloSan/LinuxIO/commit/9ebd82f)) by @MordilloSan
* fix: - improved logo in readme for both dark and light mode ([b02d305](https://github.com/mordilloSan/LinuxIO/commit/b02d305)) by @MordilloSan

### ♻️ Refactoring

* refactor: consolidate config, remove magic strings, improve CLI and logging ([e1055f9](https://github.com/mordilloSan/LinuxIO/commit/e1055f9)) by @MordilloSan

### 🏗️ Build

* build(deps-dev): bump js-yaml from 4.1.0 to 4.1.1 in /frontend ([77c0e16](https://github.com/mordilloSan/LinuxIO/commit/77c0e16)) by @dependabot[bot]

### 🔄 Other Changes

* Merge pull request #43 from mordilloSan:dependabot/npm_and_yarn/frontend/js-yaml-4.1.1 ([f2b16a4](https://github.com/mordilloSan/LinuxIO/commit/f2b16a4)) by @mordillo
* Merge branch 'main' into dev/v0.4.1 ([4396526](https://github.com/mordilloSan/LinuxIO/commit/4396526)) by @mordillo
* Merge pull request #48 from mordilloSan/dev/v0.4.1 ([098a126](https://github.com/mordilloSan/LinuxIO/commit/098a126)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @dependabot[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.4.0...v0.4.1

## v0.4.0 — 2025-12-15

### 🚀 Features

* feat: implement marquee selection feature with visual overlay ([08a407b](https://github.com/mordilloSan/LinuxIO/commit/08a407b)) by @MordilloSan
* feat: Implement file transfer functionality with drag-and-drop support ([1b84dc9](https://github.com/mordilloSan/LinuxIO/commit/1b84dc9)) by @MordilloSan
* feat: Add compression progress tracking and update file transfer handling ([a08529b](https://github.com/mordilloSan/LinuxIO/commit/a08529b)) by @MordilloSan
* feat: Implement file permission management with chmod functionality and user/group retrieval ([7bad8eb](https://github.com/mordilloSan/LinuxIO/commit/7bad8eb)) by @MordilloSan
* feat: Implement copy and cut functionality in file browser with clipboard support ([ffa761a](https://github.com/mordilloSan/LinuxIO/commit/ffa761a)) by @MordilloSan
* feat: Add file upload, update, download, and archive download functionalities with temp file handling ([e7c0217](https://github.com/mordilloSan/LinuxIO/commit/e7c0217)) by @MordilloSan
* feat: Implement streaming file upload and download via IPC ([40bf8a8](https://github.com/mordilloSan/LinuxIO/commit/40bf8a8)) by @MordilloSan
* feat: Implement framed protocol support for binary and streaming data ([3cf058d](https://github.com/mordilloSan/LinuxIO/commit/3cf058d)) by @MordilloSan
* feat: Add error handling for empty bridge output and improve error logging in session upload ([f647fe0](https://github.com/mordilloSan/LinuxIO/commit/f647fe0)) by @MordilloSan
* feat: pipe bridge logs to main program stdout in dev mode ([0d2c73a](https://github.com/mordilloSan/LinuxIO/commit/0d2c73a)) by @MordilloSan
* feat: optimize directory size fetching with batch API and search result support ([30ed87e](https://github.com/mordilloSan/LinuxIO/commit/30ed87e)) by @MordilloSan
* feat: ([8cb7581](https://github.com/mordilloSan/LinuxIO/commit/8cb7581)) by @MordilloSan

### 🐛 Bug Fixes

* fix: linting ([f86b592](https://github.com/mordilloSan/LinuxIO/commit/f86b592)) by @MordilloSan
* fix: linting ([2cba117](https://github.com/mordilloSan/LinuxIO/commit/2cba117)) by @MordilloSan
* fix: linting ([c061caa](https://github.com/mordilloSan/LinuxIO/commit/c061caa)) by @MordilloSan

### ♻️ Refactoring

* refactor: replace directory size calculation with indexer service and update related hooks ([6ac8f3b](https://github.com/mordilloSan/LinuxIO/commit/6ac8f3b)) by @MordilloSan
* refactor: Simplify PermissionsDialog component by removing unused props and optimizing key handling ([3583a42](https://github.com/mordilloSan/LinuxIO/commit/3583a42)) by @MordilloSan
* refactor: Replace CallWithSession with CallTypedWithSession for improved error handling in network handlers ([c14678a](https://github.com/mordilloSan/LinuxIO/commit/c14678a)) by @MordilloSan
* refactor: Replace CallWithSession with CallTypedWithSession for improved response handling across system and update handlers ([cd059d7](https://github.com/mordilloSan/LinuxIO/commit/cd059d7)) by @MordilloSan
* refactor: Update bridge call handling to use CallTypedWithSession for improved response parsing and error handling ([4d55168](https://github.com/mordilloSan/LinuxIO/commit/4d55168)) by @MordilloSan
* refactor: Enhance upload dialog functionality and improve file handling in FileBrowser ([d164403](https://github.com/mordilloSan/LinuxIO/commit/d164403)) by @MordilloSan
* refactor: rename userconfig package to config and update references ([dbd4049](https://github.com/mordilloSan/LinuxIO/commit/dbd4049)) by @MordilloSan
* refactor: enhance session termination handling and improve bridge failure response feat: incorporated download notifications on the left footer... ([845fdbf](https://github.com/mordilloSan/LinuxIO/commit/845fdbf)) by @MordilloSan
* refactor: enhance DownloadNotifications component with completed transfers tracking and UI improvements ([f63355e](https://github.com/mordilloSan/LinuxIO/commit/f63355e)) by @MordilloSan
* refactor: implement unsubscribe functionality for download and compression progress in WebSocketHandler feat: increase bridge kernel limits in auth-helper ([e990918](https://github.com/mordilloSan/LinuxIO/commit/e990918)) by @MordilloSan
* refactor: enhance archive handling with unique name generation and conflict resolution ([423ea64](https://github.com/mordilloSan/LinuxIO/commit/423ea64)) by @MordilloSan
* refactor: improve WebSocket subscription management and enhance compression handling ([38ecf86](https://github.com/mordilloSan/LinuxIO/commit/38ecf86)) by @MordilloSan
* refactor: streamline error handling and improve code readability in filebrowser and FileTransferContext ([81492f3](https://github.com/mordilloSan/LinuxIO/commit/81492f3)) by @MordilloSan
* refactor: optimize file archiving logic and enhance download label generation refactor: improve bridge logging on SIGKILL ([0e3d5f6](https://github.com/mordilloSan/LinuxIO/commit/0e3d5f6)) by @MordilloSan
* refactor: clean up .gitignore formatting and improve logging in linuxio-auth-helper ([f8f219d](https://github.com/mordilloSan/LinuxIO/commit/f8f219d)) by @MordilloSan
* refactor: implement streaming file upload and enhance error handling in filebrowser ([fb64a9f](https://github.com/mordilloSan/LinuxIO/commit/fb64a9f)) by @MordilloSan
* refactor: generalize progress handling and update WebSocket subscription logic ([61b866e](https://github.com/mordilloSan/LinuxIO/commit/61b866e)) by @MordilloSan
* refactor: update theme handlers to improve argument handling and logging; enhance file upload process using temporary files ([58a0b1f](https://github.com/mordilloSan/LinuxIO/commit/58a0b1f)) by @MordilloSan
* refactor: replace fileUploadStream with fileUploadFromTemp in handler mappings ([e826a08](https://github.com/mordilloSan/LinuxIO/commit/e826a08)) by @MordilloSan
* refactor: enhance archive extraction process with progress tracking and error handling ([36c2c2f](https://github.com/mordilloSan/LinuxIO/commit/36c2c2f)) by @MordilloSan
* refactor: enhance file upload process with progress tracking and request ID handling ([54939bc](https://github.com/mordilloSan/LinuxIO/commit/54939bc)) by @MordilloSan
* refactor: update file upload process with progress tracking; replace DownloadNotifications with FileNotifications component ([3259cdb](https://github.com/mordilloSan/LinuxIO/commit/3259cdb)) by @MordilloSan
* refactor: enhance file transfer context and notifications with speed tracking; implement streaming progress handling ([1ee68fb](https://github.com/mordilloSan/LinuxIO/commit/1ee68fb)) by @MordilloSan
* refactor: implement operation context management for cancellable file operations; enhance progress tracking and error handling ([4ac78fc](https://github.com/mordilloSan/LinuxIO/commit/4ac78fc)) by @MordilloSan
* refactor: enhance file upload process with display name handling and progress labeling ([4554240](https://github.com/mordilloSan/LinuxIO/commit/4554240)) by @MordilloSan
* refactor: add file update functionality from temporary files and enhance file notifications component ([899f676](https://github.com/mordilloSan/LinuxIO/commit/899f676)) by @MordilloSan
* refactor: remove unused resourcePut and rawFiles handlers; update routes for clarity ([434e53f](https://github.com/mordilloSan/LinuxIO/commit/434e53f)) by @MordilloSan
* refactor: synchronize permissions inputs with selected item on dialog open ([4a42d32](https://github.com/mordilloSan/LinuxIO/commit/4a42d32)) by @MordilloSan
* refactor: update README for FileBrowser Quantum integration; enhance file mutation error handling and path validation ([16119c6](https://github.com/mordilloSan/LinuxIO/commit/16119c6)) by @MordilloSan
* refactor: enhance file operations with overwrite functionality; update tests and permissions dialog handling ([0c36fa8](https://github.com/mordilloSan/LinuxIO/commit/0c36fa8)) by @MordilloSan
* refactor: improve error handling for identical source and destination in file operations ([b058301](https://github.com/mordilloSan/LinuxIO/commit/b058301)) by @MordilloSan
* refactor: add rename functionality to file operations; implement rename dialog and mutation handling ([f2e3dcf](https://github.com/mordilloSan/LinuxIO/commit/f2e3dcf)) by @MordilloSan
* refactor: streamline variable assignment for rename destination in FileBrowser ([f1cf34a](https://github.com/mordilloSan/LinuxIO/commit/f1cf34a)) by @MordilloSan
* refactor: update build flags in makefile to include -tags=nomsgpack and optimize ldflags ([b903cf8](https://github.com/mordilloSan/LinuxIO/commit/b903cf8)) by @MordilloSan
* refactor: wrap theme handlers with ipc.WrapSimpleHandler for improved consistency ([40be5f4](https://github.com/mordilloSan/LinuxIO/commit/40be5f4)) by @MordilloSan
* refactor: enhance archive extraction with ExtractOptions for progress reporting ([abafc05](https://github.com/mordilloSan/LinuxIO/commit/abafc05)) by @MordilloSan
* refactor: integrate indexer notifications for file operations and add indexer status check ([bd645e5](https://github.com/mordilloSan/LinuxIO/commit/bd645e5)) by @MordilloSan
* refactor: linting and stale time for filebrowser react query ([dacd079](https://github.com/mordilloSan/LinuxIO/commit/dacd079)) by @MordilloSan
* refactor: fixed dir-size display without needing to refresh the page ([576da33](https://github.com/mordilloSan/LinuxIO/commit/576da33)) by @MordilloSan
* refactor: add indexer availability tracking with circuit breaker pattern ([c45cf1a](https://github.com/mordilloSan/LinuxIO/commit/c45cf1a)) by @MordilloSan

### 🔄 Other Changes

* update ([e7bdaea](https://github.com/mordilloSan/LinuxIO/commit/e7bdaea)) by @MordilloSan
* update ([45fd701](https://github.com/mordilloSan/LinuxIO/commit/45fd701)) by @MordilloSan
* update ([0f5708f](https://github.com/mordilloSan/LinuxIO/commit/0f5708f)) by @MordilloSan
* update ([069360b](https://github.com/mordilloSan/LinuxIO/commit/069360b)) by @MordilloSan
* update ([f64c6d7](https://github.com/mordilloSan/LinuxIO/commit/f64c6d7)) by @MordilloSan
* update ([c4af8f0](https://github.com/mordilloSan/LinuxIO/commit/c4af8f0)) by @MordilloSan
* update ([c57c260](https://github.com/mordilloSan/LinuxIO/commit/c57c260)) by @MordilloSan
* update ([341a0c0](https://github.com/mordilloSan/LinuxIO/commit/341a0c0)) by @MordilloSan
* up ([90e6572](https://github.com/mordilloSan/LinuxIO/commit/90e6572)) by @MordilloSan
* update ([b6347ce](https://github.com/mordilloSan/LinuxIO/commit/b6347ce)) by @MordilloSan
* Refactor filebrowser: Remove facades and use services directly ([c9179d2](https://github.com/mordilloSan/LinuxIO/commit/c9179d2)) by @MordilloSan
* Create archive_service.go and remove archive logic from raw.go ([c38291a](https://github.com/mordilloSan/LinuxIO/commit/c38291a)) by @MordilloSan
* Add comprehensive architecture documentation ([105345a](https://github.com/mordilloSan/LinuxIO/commit/105345a)) by @MordilloSan
* Consolidate fileops and services into single package ([bc89b9d](https://github.com/mordilloSan/LinuxIO/commit/bc89b9d)) by @MordilloSan
* Add user context validation infrastructure and audit ([cba6948](https://github.com/mordilloSan/LinuxIO/commit/cba6948)) by @MordilloSan
* up ([59a0e34](https://github.com/mordilloSan/LinuxIO/commit/59a0e34)) by @MordilloSan
* update ([a557ef9](https://github.com/mordilloSan/LinuxIO/commit/a557ef9)) by @MordilloSan
* update ([8a8c382](https://github.com/mordilloSan/LinuxIO/commit/8a8c382)) by @MordilloSan
* update ([1a51ffb](https://github.com/mordilloSan/LinuxIO/commit/1a51ffb)) by @MordilloSan
* bridge migration ([98a8272](https://github.com/mordilloSan/LinuxIO/commit/98a8272)) by @MordilloSan
* update ([2b2e92e](https://github.com/mordilloSan/LinuxIO/commit/2b2e92e)) by @MordilloSan
* Implement streaming architecture for file browser with persistent socket optimization ([e8c2132](https://github.com/mordilloSan/LinuxIO/commit/e8c2132)) by @MordilloSan
* Fix folder navigation not triggering re-fetch in file browser ([6ef7238](https://github.com/mordilloSan/LinuxIO/commit/6ef7238)) by @MordilloSan
* Improve folder navigation - keep previous data while refetching ([007a111](https://github.com/mordilloSan/LinuxIO/commit/007a111)) by @MordilloSan
* Add lightweight DirectoryListingLoader component and use in filebrowser ([f1f2a05](https://github.com/mordilloSan/LinuxIO/commit/f1f2a05)) by @MordilloSan
* update ([3fe57ef](https://github.com/mordilloSan/LinuxIO/commit/3fe57ef)) by @MordilloSan
* update ([a32681b](https://github.com/mordilloSan/LinuxIO/commit/a32681b)) by @MordilloSan
* update ([2bb7ace](https://github.com/mordilloSan/LinuxIO/commit/2bb7ace)) by @MordilloSan
* update ([358be77](https://github.com/mordilloSan/LinuxIO/commit/358be77)) by @MordilloSan
* lucide update ([6d55d24](https://github.com/mordilloSan/LinuxIO/commit/6d55d24)) by @MordilloSan
* update ([3dcb088](https://github.com/mordilloSan/LinuxIO/commit/3dcb088)) by @MordilloSan
* update ([b1da9e3](https://github.com/mordilloSan/LinuxIO/commit/b1da9e3)) by @MordilloSan
* update ([14eddb3](https://github.com/mordilloSan/LinuxIO/commit/14eddb3)) by @MordilloSan
* update ([f5c1233](https://github.com/mordilloSan/LinuxIO/commit/f5c1233)) by @MordilloSan
* update ([6c660e2](https://github.com/mordilloSan/LinuxIO/commit/6c660e2)) by @MordilloSan
* update ([f058026](https://github.com/mordilloSan/LinuxIO/commit/f058026)) by @MordilloSan
* update ([aec2aaf](https://github.com/mordilloSan/LinuxIO/commit/aec2aaf)) by @MordilloSan
* up ([79ead4a](https://github.com/mordilloSan/LinuxIO/commit/79ead4a)) by @MordilloSan
* up ([cc6ca3c](https://github.com/mordilloSan/LinuxIO/commit/cc6ca3c)) by @MordilloSan
* update ([3dc47f7](https://github.com/mordilloSan/LinuxIO/commit/3dc47f7)) by @MordilloSan
* update ([c53e590](https://github.com/mordilloSan/LinuxIO/commit/c53e590)) by @MordilloSan
* update ([ac02d12](https://github.com/mordilloSan/LinuxIO/commit/ac02d12)) by @MordilloSan
* update ([8de26be](https://github.com/mordilloSan/LinuxIO/commit/8de26be)) by @MordilloSan
* update ([36c0626](https://github.com/mordilloSan/LinuxIO/commit/36c0626)) by @MordilloSan
* up ([5247c3a](https://github.com/mordilloSan/LinuxIO/commit/5247c3a)) by @MordilloSan
* u ([6b04e77](https://github.com/mordilloSan/LinuxIO/commit/6b04e77)) by @MordilloSan
* up ([c114d8b](https://github.com/mordilloSan/LinuxIO/commit/c114d8b)) by @MordilloSan
* update ([c2d67d0](https://github.com/mordilloSan/LinuxIO/commit/c2d67d0)) by @MordilloSan
* Fix terminal hang: add missing list_shells_main handler ([a782146](https://github.com/mordilloSan/LinuxIO/commit/a782146)) by @MordilloSan
* update ([bf42260](https://github.com/mordilloSan/LinuxIO/commit/bf42260)) by @MordilloSan
* - websocket and route channeling. ([be59e87](https://github.com/mordilloSan/LinuxIO/commit/be59e87)) by @MordilloSan
* code restructure ([4cbb450](https://github.com/mordilloSan/LinuxIO/commit/4cbb450)) by @MordilloSan
* Code restructure 2 ([ce3c881](https://github.com/mordilloSan/LinuxIO/commit/ce3c881)) by @MordilloSan
* code restructure 3 ([aac3b10](https://github.com/mordilloSan/LinuxIO/commit/aac3b10)) by @MordilloSan
* update ([5ceb09c](https://github.com/mordilloSan/LinuxIO/commit/5ceb09c)) by @MordilloSan
* goroutine crash fix ([c533979](https://github.com/mordilloSan/LinuxIO/commit/c533979)) by @MordilloSan
* up ([8676edf](https://github.com/mordilloSan/LinuxIO/commit/8676edf)) by @MordilloSan
* up ([db4d4e3](https://github.com/mordilloSan/LinuxIO/commit/db4d4e3)) by @MordilloSan
* editor update ([34f63ac](https://github.com/mordilloSan/LinuxIO/commit/34f63ac)) by @MordilloSan
* update ([5f02309](https://github.com/mordilloSan/LinuxIO/commit/5f02309)) by @MordilloSan
* bug fixes ([e9556fd](https://github.com/mordilloSan/LinuxIO/commit/e9556fd)) by @MordilloSan
* update all bug fix ([01f61ec](https://github.com/mordilloSan/LinuxIO/commit/01f61ec)) by @MordilloSan
* update ui fix ([4911d2c](https://github.com/mordilloSan/LinuxIO/commit/4911d2c)) by @MordilloSan
* zip compression feature ([9c38135](https://github.com/mordilloSan/LinuxIO/commit/9c38135)) by @MordilloSan
* Revert "feat: Implement streaming file upload and download via IPC" ([525af16](https://github.com/mordilloSan/LinuxIO/commit/525af16)) by @MordilloSan
* Refactor bridge handlers to use CallTypedWithSession for improved error handling and response parsing ([02878b4](https://github.com/mordilloSan/LinuxIO/commit/02878b4)) by @MordilloSan
* Refactor IPC Handler Functions to Support Streaming ([fff890e](https://github.com/mordilloSan/LinuxIO/commit/fff890e)) by @MordilloSan
* feature: ([bdd5dc6](https://github.com/mordilloSan/LinuxIO/commit/bdd5dc6)) by @MordilloSan
* linting update ([999da3d](https://github.com/mordilloSan/LinuxIO/commit/999da3d)) by @MordilloSan
* Merge pull request #47 from mordilloSan/dev/v0.4.0 ([46d29fa](https://github.com/mordilloSan/LinuxIO/commit/46d29fa)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.9...v0.4.0

## v0.3.9 — 2025-10-26

### 🚀 Features

* feat: enhance logging in development mode and update go_logger dependency ([362be53](https://github.com/mordilloSan/LinuxIO/commit/362be53)) by @MordilloSan

### 🐛 Bug Fixes

* fix: package-lock.json ([6143300](https://github.com/mordilloSan/LinuxIO/commit/6143300)) by @MordilloSan
* fix: update log directory permissions to use octal notation ([ec398e3](https://github.com/mordilloSan/LinuxIO/commit/ec398e3)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #42 from mordilloSan/dev/v0.3.9 ([581ffca](https://github.com/mordilloSan/LinuxIO/commit/581ffca)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.8...v0.3.9

## v0.3.8 — 2025-10-26

### 🚀 Features

* feat: add vite-plugin-compression for gzip support in build process ([04e345f](https://github.com/mordilloSan/LinuxIO/commit/04e345f)) by @MordilloSan
* feat: add vite-plugin-compression2 for enhanced gzip support in build process ([f6b4bda](https://github.com/mordilloSan/LinuxIO/commit/f6b4bda)) by @MordilloSan

### 🐛 Bug Fixes

* fix: remove frontend/package-lock.json from .gitignore ([1a7d6b5](https://github.com/mordilloSan/LinuxIO/commit/1a7d6b5)) by @MordilloSan

### ♻️ Refactoring

* refactor: update logger usage to structured logging across multiple files ([53bdd3f](https://github.com/mordilloSan/LinuxIO/commit/53bdd3f)) by @MordilloSan
* refactor: remove redundant log prefixes for clarity in update process ([562a0e9](https://github.com/mordilloSan/LinuxIO/commit/562a0e9)) by @MordilloSan

### 🏗️ Build

* build(deps-dev): bump vite from 7.1.9 to 7.1.11 in /frontend ([cfcddea](https://github.com/mordilloSan/LinuxIO/commit/cfcddea)) by @dependabot[bot]

### 🔄 Other Changes

* Refactor logging package usage across the backend ([31f28cb](https://github.com/mordilloSan/LinuxIO/commit/31f28cb)) by @MordilloSan
* Refactor logging in bridge handlers and update go_logger dependency ([71c41ec](https://github.com/mordilloSan/LinuxIO/commit/71c41ec)) by @MordilloSan
* Merge branch 'main' into dependabot/npm_and_yarn/frontend/vite-7.1.11 ([74fe917](https://github.com/mordilloSan/LinuxIO/commit/74fe917)) by @mordillo
* Merge pull request #39 from mordilloSan/dependabot/npm_and_yarn/frontend/vite-7.1.11 ([517cebc](https://github.com/mordilloSan/LinuxIO/commit/517cebc)) by @mordillo
* Merge branch 'main' into dev/v0.3.8 ([77f1d83](https://github.com/mordilloSan/LinuxIO/commit/77f1d83)) by @mordillo
* Merge pull request #41 from mordilloSan/dev/v0.3.8 ([5ede452](https://github.com/mordilloSan/LinuxIO/commit/5ede452)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @dependabot[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.7...v0.3.8

## v0.3.7 — 2025-10-25

### 🐛 Bug Fixes

* fix: update filebrowser ([670bf94](https://github.com/mordilloSan/LinuxIO/commit/670bf94)) by @MordilloSan

### 🔄 Other Changes

* filebrowser update ([668071c](https://github.com/mordilloSan/LinuxIO/commit/668071c)) by @MordilloSan
* Merge pull request #40 from mordilloSan/dev/v0.3.7 ([d77156d](https://github.com/mordilloSan/LinuxIO/commit/d77156d)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.6...v0.3.7

## v0.3.6 — 2025-10-17

### 🐛 Bug Fixes

* fix: small dev bug ([d50c214](https://github.com/mordilloSan/LinuxIO/commit/d50c214)) by @MordilloSan
* fix: small bug ([fddf93a](https://github.com/mordilloSan/LinuxIO/commit/fddf93a)) by @MordilloSan
* fix: small bug ([a13be58](https://github.com/mordilloSan/LinuxIO/commit/a13be58)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #38 from mordilloSan/dev/v0.3.6 ([f74189f](https://github.com/mordilloSan/LinuxIO/commit/f74189f)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.5...v0.3.6

## v0.3.5 — 2025-10-17

### 🐛 Bug Fixes

* fix: app update bug hunt ([20557b0](https://github.com/mordilloSan/LinuxIO/commit/20557b0)) by @MordilloSan
* fix: linting ([ba5f810](https://github.com/mordilloSan/LinuxIO/commit/ba5f810)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #37 from mordilloSan/dev/v0.3.5 ([33ebca7](https://github.com/mordilloSan/LinuxIO/commit/33ebca7)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.4...v0.3.5

## v0.3.4 — 2025-10-16

### 🚀 Features

* feat: service table visual ([9722094](https://github.com/mordilloSan/LinuxIO/commit/9722094)) by @MordilloSan
* feat: share page start ([4ef014b](https://github.com/mordilloSan/LinuxIO/commit/4ef014b)) by @MordilloSan
* feat: circular gauge ([7928ed8](https://github.com/mordilloSan/LinuxIO/commit/7928ed8)) by @MordilloSan

### 🐛 Bug Fixes

* fix: obsolete circular gauge  deletion ([0469e9a](https://github.com/mordilloSan/LinuxIO/commit/0469e9a)) by @MordilloSan

### 💄 Style

* style: new circular gauge ([b8313bb](https://github.com/mordilloSan/LinuxIO/commit/b8313bb)) by @MordilloSan

### 🔄 Other Changes

* improv: card spacing tune ([062a462](https://github.com/mordilloSan/LinuxIO/commit/062a462)) by @MordilloSan
* linting ([e48ec3c](https://github.com/mordilloSan/LinuxIO/commit/e48ec3c)) by @MordilloSan
* Merge pull request #36 from mordilloSan/dev/v0.3.4 ([7fe9a7d](https://github.com/mordilloSan/LinuxIO/commit/7fe9a7d)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.3...v0.3.4

## v0.3.3 — 2025-10-16

### 🐛 Bug Fixes

* fix: download folder bug ([6eace6c](https://github.com/mordilloSan/LinuxIO/commit/6eace6c)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #35 from mordilloSan/dev/v0.3.3 ([870ccab](https://github.com/mordilloSan/LinuxIO/commit/870ccab)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.2...v0.3.3

## v0.3.2 — 2025-10-16

### 🚀 Features

* feat: filebrowser in dev mode ([f8aead9](https://github.com/mordilloSan/LinuxIO/commit/f8aead9)) by @MordilloSan

### 🐛 Bug Fixes

* fix: temperature report on components bug ([09f173c](https://github.com/mordilloSan/LinuxIO/commit/09f173c)) by @MordilloSan
* fix: dev mode non sudo login ([ddfb835](https://github.com/mordilloSan/LinuxIO/commit/ddfb835)) by @MordilloSan
* fix: outdated script ([4b0c56c](https://github.com/mordilloSan/LinuxIO/commit/4b0c56c)) by @MordilloSan
* fix: filebrowser docker container cleanup ([547de5d](https://github.com/mordilloSan/LinuxIO/commit/547de5d)) by @MordilloSan

### ⚡ Performance

* perf: theming update ([ab89e78](https://github.com/mordilloSan/LinuxIO/commit/ab89e78)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #34 from mordilloSan/dev/v0.3.2 ([59c5350](https://github.com/mordilloSan/LinuxIO/commit/59c5350)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.1...v0.3.2

## v0.3.1 — 2025-10-15

### 🐛 Bug Fixes

* fix: update timeout ([d794980](https://github.com/mordilloSan/LinuxIO/commit/d794980)) by @MordilloSan
* fix: NIC now prefills values when setting manual option ([170f84b](https://github.com/mordilloSan/LinuxIO/commit/170f84b)) by @MordilloSan
* fix: linting ([db80d24](https://github.com/mordilloSan/LinuxIO/commit/db80d24)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #33 from mordilloSan/dev/v0.3.1 ([f5ccb7b](https://github.com/mordilloSan/LinuxIO/commit/f5ccb7b)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.3.0...v0.3.1

## v0.3.0 — 2025-10-15

### 🚀 Features

* feat: big service page update with action functional ([977e717](https://github.com/mordilloSan/LinuxIO/commit/977e717)) by @MordilloSan

### 🐛 Bug Fixes

* fix: linting ([738d862](https://github.com/mordilloSan/LinuxIO/commit/738d862)) by @MordilloSan
* fix:code cleaning ([47b9624](https://github.com/mordilloSan/LinuxIO/commit/47b9624)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #32 from mordilloSan/dev/v0.3.0 ([e52fce8](https://github.com/mordilloSan/LinuxIO/commit/e52fce8)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.33...v0.3.0

## v0.2.33 — 2025-10-15

### 🔄 Other Changes

* app update refresh ([5870c6c](https://github.com/mordilloSan/LinuxIO/commit/5870c6c)) by @MordilloSan
* Merge pull request #31 from mordilloSan/dev/v0.2.33 ([6dcea7e](https://github.com/mordilloSan/LinuxIO/commit/6dcea7e)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.32...v0.2.33

## v0.2.32 — 2025-10-15

### 🐛 Bug Fixes

* fix: better script logging ([b5ad425](https://github.com/mordilloSan/LinuxIO/commit/b5ad425)) by @MordilloSan

### 🔄 Other Changes

* Update global_install.sh ([4e72f1c](https://github.com/mordilloSan/LinuxIO/commit/4e72f1c)) by @mordillo
* Merge branch 'main' into dev/v0.2.32 ([51ea05a](https://github.com/mordilloSan/LinuxIO/commit/51ea05a)) by @mordillo
* Merge pull request #30 from mordilloSan/dev/v0.2.32 ([4845f54](https://github.com/mordilloSan/LinuxIO/commit/4845f54)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.31...v0.2.32

## v0.2.31 — 2025-10-15

### 🔄 Other Changes

* readme update ([d523487](https://github.com/mordilloSan/LinuxIO/commit/d523487)) by @MordilloSan
* Merge pull request #29 from mordilloSan/dev/v0.2.31 ([88db9b8](https://github.com/mordilloSan/LinuxIO/commit/88db9b8)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.30...v0.2.31

## v0.2.30 — 2025-10-15

### 🐛 Bug Fixes

* fix: app install bug ([692be78](https://github.com/mordilloSan/LinuxIO/commit/692be78)) by @MordilloSan

### 🔄 Other Changes

* up ([3be5b3d](https://github.com/mordilloSan/LinuxIO/commit/3be5b3d)) by @MordilloSan
* script update ([db190f6](https://github.com/mordilloSan/LinuxIO/commit/db190f6)) by @MordilloSan
* update ([a4c1c9a](https://github.com/mordilloSan/LinuxIO/commit/a4c1c9a)) by @MordilloSan
* update ([b6c1ce6](https://github.com/mordilloSan/LinuxIO/commit/b6c1ce6)) by @MordilloSan
* update ([5456cb8](https://github.com/mordilloSan/LinuxIO/commit/5456cb8)) by @MordilloSan
* Merge pull request #28 from mordilloSan/dev/v0.2.30 ([3bf8d7f](https://github.com/mordilloSan/LinuxIO/commit/3bf8d7f)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.29...v0.2.30

## v0.2.29 — 2025-10-15

### 🐛 Bug Fixes

* fix: debug and test for app update ([409c297](https://github.com/mordilloSan/LinuxIO/commit/409c297)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #27 from mordilloSan/dev/v0.2.29 ([62f02fb](https://github.com/mordilloSan/LinuxIO/commit/62f02fb)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.28...v0.2.29

## v0.2.28 — 2025-10-15

### 🐛 Bug Fixes

* fix: readme typo ([35437dd](https://github.com/mordilloSan/LinuxIO/commit/35437dd)) by @MordilloSan
* fix: update debug ([cfb8eca](https://github.com/mordilloSan/LinuxIO/commit/cfb8eca)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #26 from mordilloSan/dev/v0.2.28 ([9dd9fbd](https://github.com/mordilloSan/LinuxIO/commit/9dd9fbd)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.27...v0.2.28

## v0.2.27 — 2025-10-15

### 🐛 Bug Fixes

* fix: script path fix ([f894c2c](https://github.com/mordilloSan/LinuxIO/commit/f894c2c)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #25 from mordilloSan/dev/v0.2.27 ([a9d2875](https://github.com/mordilloSan/LinuxIO/commit/a9d2875)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.26...v0.2.27

## v0.2.26 — 2025-10-15

### 🐛 Bug Fixes

* fix: app update bug ([ff05a09](https://github.com/mordilloSan/LinuxIO/commit/ff05a09)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #24 from mordilloSan/dev/v0.2.26 ([3ee3dd0](https://github.com/mordilloSan/LinuxIO/commit/3ee3dd0)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.25...v0.2.26

## v0.2.25 — 2025-10-15

### 🚀 Features

* feat: Footer Versioning fix: coherence of axios and react query ([688e6a4](https://github.com/mordilloSan/LinuxIO/commit/688e6a4)) by @MordilloSan

### 🐛 Bug Fixes

* fix: linting ([51219db](https://github.com/mordilloSan/LinuxIO/commit/51219db)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #23 from mordilloSan/dev/v0.2.25 ([d0d0c91](https://github.com/mordilloSan/LinuxIO/commit/d0d0c91)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.24...v0.2.25

## v0.2.24 — 2025-10-15

### 🐛 Bug Fixes

* fix: update script ([375c008](https://github.com/mordilloSan/LinuxIO/commit/375c008)) by @MordilloSan

### 🔄 Other Changes

* Merge pull request #22 from mordilloSan/dev/v0.2.24 ([bb3f3e6](https://github.com/mordilloSan/LinuxIO/commit/bb3f3e6)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.23...v0.2.24

## v0.2.23 — 2025-10-15

### 🔄 Other Changes

* debug: app update script bug ([6ec2a6d](https://github.com/mordilloSan/LinuxIO/commit/6ec2a6d)) by @MordilloSan
* Merge pull request #21 from mordilloSan/dev/v0.2.23 ([ec31cae](https://github.com/mordilloSan/LinuxIO/commit/ec31cae)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.22...v0.2.23

## v0.2.22 — 2025-10-15

### 🚀 Features

* feat: changelog workflow ([cd66905](https://github.com/mordilloSan/LinuxIO/commit/cd66905)) by @MordilloSan
* feat: changelog improvement ([a6bf61e](https://github.com/mordilloSan/LinuxIO/commit/a6bf61e)) by @MordilloSan
* feat: go mod tidy ([fd7e18c](https://github.com/mordilloSan/LinuxIO/commit/fd7e18c)) by @MordilloSan

### 🐛 Bug Fixes

* fix: changelog bugfix ([c40678e](https://github.com/mordilloSan/LinuxIO/commit/c40678e)) by @MordilloSan
* fix: changelog fix ([4f4ec0c](https://github.com/mordilloSan/LinuxIO/commit/4f4ec0c)) by @MordilloSan

### 🔄 Other Changes

* app update bug fix ([4cd0e2f](https://github.com/mordilloSan/LinuxIO/commit/4cd0e2f)) by @MordilloSan
* changelog improvement ([3ca6a26](https://github.com/mordilloSan/LinuxIO/commit/3ca6a26)) by @MordilloSan
* dependencie updates ([7d7d499](https://github.com/mordilloSan/LinuxIO/commit/7d7d499)) by @MordilloSan
* makefile update ([85077dd](https://github.com/mordilloSan/LinuxIO/commit/85077dd)) by @MordilloSan
* changelog fix ([cc6e6a6](https://github.com/mordilloSan/LinuxIO/commit/cc6e6a6)) by @MordilloSan
* fix makefile bug ([371d714](https://github.com/mordilloSan/LinuxIO/commit/371d714)) by @MordilloSan
* Merge pull request #20 from mordilloSan/dev/v0.2.22 ([1622cb7](https://github.com/mordilloSan/LinuxIO/commit/1622cb7)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.21...v0.2.22

## v0.2.21 — 2025-10-14

### 🔄 Other Changes

* go reportcard readme update ([1d18b5e](https://github.com/mordilloSan/LinuxIO/commit/1d18b5e)) by @MordilloSan
* testing workflow and general linting ([0456cce](https://github.com/mordilloSan/LinuxIO/commit/0456cce)) by @MordilloSan
* app update fix attempt ([9c49eed](https://github.com/mordilloSan/LinuxIO/commit/9c49eed)) by @MordilloSan
* readme update ([af60e75](https://github.com/mordilloSan/LinuxIO/commit/af60e75)) by @MordilloSan
* linting ([d441960](https://github.com/mordilloSan/LinuxIO/commit/d441960)) by @MordilloSan
* Merge pull request #19 from mordilloSan/dev/v0.2.21 ([3ee685b](https://github.com/mordilloSan/LinuxIO/commit/3ee685b)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.20...v0.2.21

## v0.2.20 — 2025-10-14

### 🔄 Other Changes

* Atualizar o go.mod ([9ca7f8e](https://github.com/mordilloSan/LinuxIO/commit/9ca7f8e)) by @mordillo
* go path fix ([01c0a4f](https://github.com/mordilloSan/LinuxIO/commit/01c0a4f)) by @MordilloSan
* go path bug fix ([ce91a24](https://github.com/mordilloSan/LinuxIO/commit/ce91a24)) by @MordilloSan
* Merge pull request #18 from mordilloSan/dev/v0.2.20 ([ae8459d](https://github.com/mordilloSan/LinuxIO/commit/ae8459d)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.16...v0.2.20

## v0.2.16 — 2025-10-14

### 🔄 Other Changes

* app update bugfix ([58bbe18](https://github.com/mordilloSan/LinuxIO/commit/58bbe18)) by @MordilloSan
* file permissions bug ([6312ef3](https://github.com/mordilloSan/LinuxIO/commit/6312ef3)) by @MordilloSan
* Merge pull request #17 from mordilloSan/dev/v0.2.16 ([1e6dc95](https://github.com/mordilloSan/LinuxIO/commit/1e6dc95)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.15...v0.2.16

## v0.2.15 — 2025-10-14

### 🔄 Other Changes

* makefile merge workflow update ([7755ee9](https://github.com/mordilloSan/LinuxIO/commit/7755ee9)) by @MordilloSan
* toast duration fix ([03ebea8](https://github.com/mordilloSan/LinuxIO/commit/03ebea8)) by @MordilloSan
* Merge pull request #16 from mordilloSan/dev/v0.2.15 ([1295494](https://github.com/mordilloSan/LinuxIO/commit/1295494)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.14...v0.2.15

## v0.2.14 — 2025-10-14

### 🔄 Other Changes

* app autoupdate ([55d7168](https://github.com/mordilloSan/LinuxIO/commit/55d7168)) by @MordilloSan
* Merge pull request #15 from mordilloSan/dev/v0.2.14 ([e3f37a7](https://github.com/mordilloSan/LinuxIO/commit/e3f37a7)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.13...v0.2.14

## v0.2.13 — 2025-10-14

### 🔄 Other Changes

* update banner ([65baf24](https://github.com/mordilloSan/LinuxIO/commit/65baf24)) by @MordilloSan
* makefile update ([a410fd3](https://github.com/mordilloSan/LinuxIO/commit/a410fd3)) by @MordilloSan
* makefile update ([917e569](https://github.com/mordilloSan/LinuxIO/commit/917e569)) by @MordilloSan
* Merge pull request #13 from mordilloSan/dev/v0.2.12 ([0203e18](https://github.com/mordilloSan/LinuxIO/commit/0203e18)) by @mordillo
* makefile update ([e1c9825](https://github.com/mordilloSan/LinuxIO/commit/e1c9825)) by @MordilloSan
* makefile update ([ee1d3d0](https://github.com/mordilloSan/LinuxIO/commit/ee1d3d0)) by @MordilloSan
* Merge pull request #14 from mordilloSan/dev/v0.2.13 ([fe97083](https://github.com/mordilloSan/LinuxIO/commit/fe97083)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.12...v0.2.13

## v0.2.12 — 2025-10-14

### 🔄 Other Changes

* Bump github.com/quic-go/quic-go from 0.54.0 to 0.54.1 in /backend ([c58abf4](https://github.com/mordilloSan/LinuxIO/commit/c58abf4)) by @dependabot[bot]
* Merge branch 'main' into dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1 ([93d0426](https://github.com/mordilloSan/LinuxIO/commit/93d0426)) by @mordillo
* Merge remote-tracking branch 'origin/dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1' into dev/v0.2.12 ([acf2e52](https://github.com/mordilloSan/LinuxIO/commit/acf2e52)) by @MordilloSan
* Merge branch 'main' into dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1 ([e33c26b](https://github.com/mordilloSan/LinuxIO/commit/e33c26b)) by @mordillo
* Merge pull request #6 from mordilloSan/dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1 ([ce141c8](https://github.com/mordilloSan/LinuxIO/commit/ce141c8)) by @mordillo
* install process and makefile optimization ([b9cdf2d](https://github.com/mordilloSan/LinuxIO/commit/b9cdf2d)) by @MordilloSan
* makefile update ([d226403](https://github.com/mordilloSan/LinuxIO/commit/d226403)) by @MordilloSan
* makefile upgrade ([9c1f218](https://github.com/mordilloSan/LinuxIO/commit/9c1f218)) by @MordilloSan
* makefile upgrade ([e484b97](https://github.com/mordilloSan/LinuxIO/commit/e484b97)) by @MordilloSan
* makefile upgrade ([e91edb8](https://github.com/mordilloSan/LinuxIO/commit/e91edb8)) by @MordilloSan
* makefile upgrade ([0ba88c0](https://github.com/mordilloSan/LinuxIO/commit/0ba88c0)) by @MordilloSan
* up ([ed13d0b](https://github.com/mordilloSan/LinuxIO/commit/ed13d0b)) by @MordilloSan
* update ([76e32ec](https://github.com/mordilloSan/LinuxIO/commit/76e32ec)) by @MordilloSan
* make update ([5a22e8f](https://github.com/mordilloSan/LinuxIO/commit/5a22e8f)) by @MordilloSan
* bug fix ([b69ceb9](https://github.com/mordilloSan/LinuxIO/commit/b69ceb9)) by @MordilloSan
* bug fix ([cdadb91](https://github.com/mordilloSan/LinuxIO/commit/cdadb91)) by @MordilloSan
* test ([fcec6db](https://github.com/mordilloSan/LinuxIO/commit/fcec6db)) by @MordilloSan
* bug fix ([2bc290f](https://github.com/mordilloSan/LinuxIO/commit/2bc290f)) by @MordilloSan
* up ([a87101d](https://github.com/mordilloSan/LinuxIO/commit/a87101d)) by @MordilloSan
* up ([d76e08b](https://github.com/mordilloSan/LinuxIO/commit/d76e08b)) by @MordilloSan
* bug fix ([9fc5a78](https://github.com/mordilloSan/LinuxIO/commit/9fc5a78)) by @MordilloSan
* bug fix ([29bfb26](https://github.com/mordilloSan/LinuxIO/commit/29bfb26)) by @MordilloSan
* bug fix ([981bacc](https://github.com/mordilloSan/LinuxIO/commit/981bacc)) by @MordilloSan
* Merge pull request #12 from mordilloSan/dev/v0.2.12 ([2ec37b3](https://github.com/mordilloSan/LinuxIO/commit/2ec37b3)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @dependabot[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.11...v0.2.12

## v0.2.11 — 2025-10-13

### 🔄 Other Changes

* updatebanner bug ([26d0e10](https://github.com/mordilloSan/LinuxIO/commit/26d0e10)) by @MordilloSan
* Merge pull request #11 from mordilloSan/dev/v0.2.11 ([8e4ff6e](https://github.com/mordilloSan/LinuxIO/commit/8e4ff6e)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.10...v0.2.11

## v0.2.10 — 2025-10-13

### 🔄 Other Changes

* version API ([8b7add0](https://github.com/mordilloSan/LinuxIO/commit/8b7add0)) by @MordilloSan
* Merge pull request #10 from mordilloSan/dev/v0.2.10 ([38b6c7f](https://github.com/mordilloSan/LinuxIO/commit/38b6c7f)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.9...v0.2.10

## v0.2.9 — 2025-10-13

### 🔄 Other Changes

* app update procedure ([49c3ef1](https://github.com/mordilloSan/LinuxIO/commit/49c3ef1)) by @MordilloSan
* linting fix ([c05e15d](https://github.com/mordilloSan/LinuxIO/commit/c05e15d)) by @MordilloSan
* Merge pull request #9 from mordilloSan/dev/v0.2.9 ([6471ea3](https://github.com/mordilloSan/LinuxIO/commit/6471ea3)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.8...v0.2.9

## v0.2.8 — 2025-10-13

### 🔄 Other Changes

* automatic updates ([61ccf00](https://github.com/mordilloSan/LinuxIO/commit/61ccf00)) by @MordilloSan
* linting ([6048b8d](https://github.com/mordilloSan/LinuxIO/commit/6048b8d)) by @MordilloSan
* Merge pull request #8 from mordilloSan/dev/v0.2.8 ([5fc9044](https://github.com/mordilloSan/LinuxIO/commit/5fc9044)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.6...v0.2.8

## v0.2.6 — 2025-10-11

### 📚 Documentation

* docs: update changelog for v0.2.6 ([8e4829a](https://github.com/mordilloSan/LinuxIO/commit/8e4829a)) by @MordilloSan
* docs: update changelog for v0.2.6 ([c7c95b9](https://github.com/mordilloSan/LinuxIO/commit/c7c95b9)) by @MordilloSan

### 🔄 Other Changes

* versioning update ([84590ec](https://github.com/mordilloSan/LinuxIO/commit/84590ec)) by @MordilloSan
* websocket ([6f0f9e0](https://github.com/mordilloSan/LinuxIO/commit/6f0f9e0)) by @MordilloSan
* env cleanup ([4305bbe](https://github.com/mordilloSan/LinuxIO/commit/4305bbe)) by @MordilloSan
* env bug fix ([8020e12](https://github.com/mordilloSan/LinuxIO/commit/8020e12)) by @MordilloSan
* env bug fix ([b0d34c6](https://github.com/mordilloSan/LinuxIO/commit/b0d34c6)) by @MordilloSan
* socket determination update enviorment variables removal C helper update ([0c9c973](https://github.com/mordilloSan/LinuxIO/commit/0c9c973)) by @MordilloSan
* changelog update ([8dedd7c](https://github.com/mordilloSan/LinuxIO/commit/8dedd7c)) by @MordilloSan
* changelog update ([25e114f](https://github.com/mordilloSan/LinuxIO/commit/25e114f)) by @MordilloSan
* makefile changelog code ([7bc5046](https://github.com/mordilloSan/LinuxIO/commit/7bc5046)) by @MordilloSan
* makefile bugfix ([bbcb515](https://github.com/mordilloSan/LinuxIO/commit/bbcb515)) by @MordilloSan
* makefile improvement ([0bf2ad8](https://github.com/mordilloSan/LinuxIO/commit/0bf2ad8)) by @MordilloSan
* makefile update ([c49e945](https://github.com/mordilloSan/LinuxIO/commit/c49e945)) by @MordilloSan
* changelog update ([1ac238f](https://github.com/mordilloSan/LinuxIO/commit/1ac238f)) by @MordilloSan
* pull request workflow ([a6c0382](https://github.com/mordilloSan/LinuxIO/commit/a6c0382)) by @MordilloSan
* Merge pull request #7 from mordilloSan/dev/v0.2.6 ([f2a54d7](https://github.com/mordilloSan/LinuxIO/commit/f2a54d7)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.5...v0.2.6

## v0.2.5 — 2025-10-09

### 🔄 Other Changes

* package updater refreshed ([0bb7a92](https://github.com/mordilloSan/LinuxIO/commit/0bb7a92)) by @MordilloSan
* Merge pull request #5 from mordilloSan/dev/v0.2.5 ([a530b05](https://github.com/mordilloSan/LinuxIO/commit/a530b05)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.3...v0.2.5

## v0.2.3 — 2025-10-08

### 🔄 Other Changes

* testing update ([a26ffb0](https://github.com/mordilloSan/LinuxIO/commit/a26ffb0)) by @MordilloSan
* golinting update ([304d5f8](https://github.com/mordilloSan/LinuxIO/commit/304d5f8)) by @MordilloSan
* github workflow update ([8eb8383](https://github.com/mordilloSan/LinuxIO/commit/8eb8383)) by @MordilloSan
* linting update ([052a9d1](https://github.com/mordilloSan/LinuxIO/commit/052a9d1)) by @MordilloSan
* Merge pull request #4 from mordilloSan/dev/v0.2.3 ([a677459](https://github.com/mordilloSan/LinuxIO/commit/a677459)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.2...v0.2.3

## v0.2.2 — 2025-10-08

### 🔄 Other Changes

* pullrequest testing workflow update ([462bf3c](https://github.com/mordilloSan/LinuxIO/commit/462bf3c)) by @MordilloSan
* makefile bugfix ([c96f47c](https://github.com/mordilloSan/LinuxIO/commit/c96f47c)) by @MordilloSan
* test workflow ([06d95fe](https://github.com/mordilloSan/LinuxIO/commit/06d95fe)) by @MordilloSan
* update to the workflow ([6baed35](https://github.com/mordilloSan/LinuxIO/commit/6baed35)) by @MordilloSan
* Merge pull request #3 from mordilloSan/dev/v0.2.2 ([40bcd3a](https://github.com/mordilloSan/LinuxIO/commit/40bcd3a)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.1...v0.2.2

## v0.2.1 — 2025-10-08

### 🔄 Other Changes

* codeql unit conversion fixes ([5a6b011](https://github.com/mordilloSan/LinuxIO/commit/5a6b011)) by @MordilloSan
* Merge pull request #2 from mordilloSan/dev/v0.2.1 ([3af2818](https://github.com/mordilloSan/LinuxIO/commit/3af2818)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.2.0...v0.2.1

## v0.2.0 — 2025-10-08

### 🐛 Bug Fixes

* fix(ci): exclude bot commits from changelog generation ([b870c42](https://github.com/mordilloSan/LinuxIO/commit/b870c42)) by @MordilloSan

### 🔄 Other Changes

* Update to the changelog workflow ([d9bac52](https://github.com/mordilloSan/LinuxIO/commit/d9bac52)) by @MordilloSan
* readme update ([857bc16](https://github.com/mordilloSan/LinuxIO/commit/857bc16)) by @MordilloSan
* makefile update ([5ff8ee6](https://github.com/mordilloSan/LinuxIO/commit/5ff8ee6)) by @MordilloSan
* makefile bugfix ([9d41405](https://github.com/mordilloSan/LinuxIO/commit/9d41405)) by @MordilloSan
* Merge pull request #1 from mordilloSan/dev/v0.2.0 ([53a87b5](https://github.com/mordilloSan/LinuxIO/commit/53a87b5)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/v0.1.0...v0.2.0

## v0.1.0 — 2025-10-08

### 🔄 Other Changes

* Initial commit - LinuxIO v0.1.0 ([a8180e2](https://github.com/mordilloSan/LinuxIO/commit/a8180e2)) by @MordilloSan
* update ([a6f3cab](https://github.com/mordilloSan/LinuxIO/commit/a6f3cab)) by @MordilloSan
* update ([5712112](https://github.com/mordilloSan/LinuxIO/commit/5712112)) by @MordilloSan
* update ([de4f213](https://github.com/mordilloSan/LinuxIO/commit/de4f213)) by @MordilloSan

### 👥 Contributors

* @MordilloSan


**Full Changelog**: https://github.com/mordilloSan/LinuxIO/compare/...v0.1.0
