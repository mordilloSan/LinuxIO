
## v0.5.1 — 2025-12-22

### 🚀 Features

* feat: -websocket and streaming reconnection and keepalive ([dd89a2c](https://github.com/owner/repo/commit/dd89a2c)) by @MordilloSan

### 🐛 Bug Fixes

* fix: - linting ([ff50f41](https://github.com/owner/repo/commit/ff50f41)) by @MordilloSan

### ♻️ Refactoring

* refactor: - old IPC code cleanup fix: - bridge now allows sudo ([224d340](https://github.com/owner/repo/commit/224d340)) by @MordilloSan
* refactor: - IPC code cleanup fix: - theming get bugfix ([a1c002d](https://github.com/owner/repo/commit/a1c002d)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.5.0...v0.5.1


## v0.5.0 — 2025-12-22

### 🐛 Bug Fixes

* fix: - Vite build process fix ([41deae0](https://github.com/owner/repo/commit/41deae0)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.4.3...v0.5.0


## v0.4.0 — 2025-12-15

### 🚀 Features

* feat: implement marquee selection feature with visual overlay ([08a407b](https://github.com/owner/repo/commit/08a407b)) by @MordilloSan
* feat: Implement file transfer functionality with drag-and-drop support ([1b84dc9](https://github.com/owner/repo/commit/1b84dc9)) by @MordilloSan
* feat: Add compression progress tracking and update file transfer handling ([a08529b](https://github.com/owner/repo/commit/a08529b)) by @MordilloSan
* feat: Implement file permission management with chmod functionality and user/group retrieval ([7bad8eb](https://github.com/owner/repo/commit/7bad8eb)) by @MordilloSan
* feat: Implement copy and cut functionality in file browser with clipboard support ([ffa761a](https://github.com/owner/repo/commit/ffa761a)) by @MordilloSan
* feat: Add file upload, update, download, and archive download functionalities with temp file handling ([e7c0217](https://github.com/owner/repo/commit/e7c0217)) by @MordilloSan
* feat: Implement streaming file upload and download via IPC ([40bf8a8](https://github.com/owner/repo/commit/40bf8a8)) by @MordilloSan
* feat: Implement framed protocol support for binary and streaming data ([3cf058d](https://github.com/owner/repo/commit/3cf058d)) by @MordilloSan
* feat: Add error handling for empty bridge output and improve error logging in session upload ([f647fe0](https://github.com/owner/repo/commit/f647fe0)) by @MordilloSan
* feat: pipe bridge logs to main program stdout in dev mode ([0d2c73a](https://github.com/owner/repo/commit/0d2c73a)) by @MordilloSan
* feat: optimize directory size fetching with batch API and search result support ([30ed87e](https://github.com/owner/repo/commit/30ed87e)) by @MordilloSan
* feat: ([8cb7581](https://github.com/owner/repo/commit/8cb7581)) by @MordilloSan

### ♻️ Refactoring

* refactor: replace directory size calculation with indexer service and update related hooks ([6ac8f3b](https://github.com/owner/repo/commit/6ac8f3b)) by @MordilloSan
* refactor: Simplify PermissionsDialog component by removing unused props and optimizing key handling ([3583a42](https://github.com/owner/repo/commit/3583a42)) by @MordilloSan
* refactor: Replace CallWithSession with CallTypedWithSession for improved error handling in network handlers ([c14678a](https://github.com/owner/repo/commit/c14678a)) by @MordilloSan
* refactor: Replace CallWithSession with CallTypedWithSession for improved response handling across system and update handlers ([cd059d7](https://github.com/owner/repo/commit/cd059d7)) by @MordilloSan
* refactor: Update bridge call handling to use CallTypedWithSession for improved response parsing and error handling ([4d55168](https://github.com/owner/repo/commit/4d55168)) by @MordilloSan
* refactor: Enhance upload dialog functionality and improve file handling in FileBrowser ([d164403](https://github.com/owner/repo/commit/d164403)) by @MordilloSan
* refactor: rename userconfig package to config and update references ([dbd4049](https://github.com/owner/repo/commit/dbd4049)) by @MordilloSan
* refactor: enhance session termination handling and improve bridge failure response feat: incorporated download notifications on the left footer... ([845fdbf](https://github.com/owner/repo/commit/845fdbf)) by @MordilloSan
* refactor: enhance DownloadNotifications component with completed transfers tracking and UI improvements ([f63355e](https://github.com/owner/repo/commit/f63355e)) by @MordilloSan
* refactor: implement unsubscribe functionality for download and compression progress in WebSocketHandler feat: increase bridge kernel limits in auth-helper ([e990918](https://github.com/owner/repo/commit/e990918)) by @MordilloSan
* refactor: enhance archive handling with unique name generation and conflict resolution ([423ea64](https://github.com/owner/repo/commit/423ea64)) by @MordilloSan
* refactor: improve WebSocket subscription management and enhance compression handling ([38ecf86](https://github.com/owner/repo/commit/38ecf86)) by @MordilloSan
* refactor: streamline error handling and improve code readability in filebrowser and FileTransferContext ([81492f3](https://github.com/owner/repo/commit/81492f3)) by @MordilloSan
* refactor: optimize file archiving logic and enhance download label generation refactor: improve bridge logging on SIGKILL ([0e3d5f6](https://github.com/owner/repo/commit/0e3d5f6)) by @MordilloSan
* refactor: clean up .gitignore formatting and improve logging in linuxio-auth-helper ([f8f219d](https://github.com/owner/repo/commit/f8f219d)) by @MordilloSan
* refactor: implement streaming file upload and enhance error handling in filebrowser ([fb64a9f](https://github.com/owner/repo/commit/fb64a9f)) by @MordilloSan
* refactor: generalize progress handling and update WebSocket subscription logic ([61b866e](https://github.com/owner/repo/commit/61b866e)) by @MordilloSan
* refactor: update theme handlers to improve argument handling and logging; enhance file upload process using temporary files ([58a0b1f](https://github.com/owner/repo/commit/58a0b1f)) by @MordilloSan
* refactor: replace fileUploadStream with fileUploadFromTemp in handler mappings ([e826a08](https://github.com/owner/repo/commit/e826a08)) by @MordilloSan
* refactor: enhance archive extraction process with progress tracking and error handling ([36c2c2f](https://github.com/owner/repo/commit/36c2c2f)) by @MordilloSan
* refactor: enhance file upload process with progress tracking and request ID handling ([54939bc](https://github.com/owner/repo/commit/54939bc)) by @MordilloSan
* refactor: update file upload process with progress tracking; replace DownloadNotifications with FileNotifications component ([3259cdb](https://github.com/owner/repo/commit/3259cdb)) by @MordilloSan
* refactor: enhance file transfer context and notifications with speed tracking; implement streaming progress handling ([1ee68fb](https://github.com/owner/repo/commit/1ee68fb)) by @MordilloSan
* refactor: implement operation context management for cancellable file operations; enhance progress tracking and error handling ([4ac78fc](https://github.com/owner/repo/commit/4ac78fc)) by @MordilloSan
* refactor: enhance file upload process with display name handling and progress labeling ([4554240](https://github.com/owner/repo/commit/4554240)) by @MordilloSan
* refactor: add file update functionality from temporary files and enhance file notifications component ([899f676](https://github.com/owner/repo/commit/899f676)) by @MordilloSan
* refactor: remove unused resourcePut and rawFiles handlers; update routes for clarity ([434e53f](https://github.com/owner/repo/commit/434e53f)) by @MordilloSan
* refactor: synchronize permissions inputs with selected item on dialog open ([4a42d32](https://github.com/owner/repo/commit/4a42d32)) by @MordilloSan
* refactor: update README for FileBrowser Quantum integration; enhance file mutation error handling and path validation ([16119c6](https://github.com/owner/repo/commit/16119c6)) by @MordilloSan
* refactor: enhance file operations with overwrite functionality; update tests and permissions dialog handling ([0c36fa8](https://github.com/owner/repo/commit/0c36fa8)) by @MordilloSan
* refactor: improve error handling for identical source and destination in file operations ([b058301](https://github.com/owner/repo/commit/b058301)) by @MordilloSan
* refactor: add rename functionality to file operations; implement rename dialog and mutation handling ([f2e3dcf](https://github.com/owner/repo/commit/f2e3dcf)) by @MordilloSan
* refactor: streamline variable assignment for rename destination in FileBrowser ([f1cf34a](https://github.com/owner/repo/commit/f1cf34a)) by @MordilloSan
* refactor: update build flags in makefile to include -tags=nomsgpack and optimize ldflags ([b903cf8](https://github.com/owner/repo/commit/b903cf8)) by @MordilloSan
* refactor: wrap theme handlers with ipc.WrapSimpleHandler for improved consistency ([40be5f4](https://github.com/owner/repo/commit/40be5f4)) by @MordilloSan
* refactor: enhance archive extraction with ExtractOptions for progress reporting ([abafc05](https://github.com/owner/repo/commit/abafc05)) by @MordilloSan
* refactor: integrate indexer notifications for file operations and add indexer status check ([bd645e5](https://github.com/owner/repo/commit/bd645e5)) by @MordilloSan
* refactor: linting and stale time for filebrowser react query ([dacd079](https://github.com/owner/repo/commit/dacd079)) by @MordilloSan
* refactor: fixed dir-size display without needing to refresh the page ([576da33](https://github.com/owner/repo/commit/576da33)) by @MordilloSan
* refactor: add indexer availability tracking with circuit breaker pattern ([c45cf1a](https://github.com/owner/repo/commit/c45cf1a)) by @MordilloSan

### 🔄 Other Changes

* Refactor filebrowser: Remove facades and use services directly ([c9179d2](https://github.com/owner/repo/commit/c9179d2)) by @MordilloSan
* Create archive_service.go and remove archive logic from raw.go ([c38291a](https://github.com/owner/repo/commit/c38291a)) by @MordilloSan
* Add comprehensive architecture documentation ([105345a](https://github.com/owner/repo/commit/105345a)) by @MordilloSan
* Consolidate fileops and services into single package ([bc89b9d](https://github.com/owner/repo/commit/bc89b9d)) by @MordilloSan
* Add user context validation infrastructure and audit ([cba6948](https://github.com/owner/repo/commit/cba6948)) by @MordilloSan
* bridge migration ([98a8272](https://github.com/owner/repo/commit/98a8272)) by @MordilloSan
* Implement streaming architecture for file browser with persistent socket optimization ([e8c2132](https://github.com/owner/repo/commit/e8c2132)) by @MordilloSan
* Fix folder navigation not triggering re-fetch in file browser ([6ef7238](https://github.com/owner/repo/commit/6ef7238)) by @MordilloSan
* Improve folder navigation - keep previous data while refetching ([007a111](https://github.com/owner/repo/commit/007a111)) by @MordilloSan
* Add lightweight DirectoryListingLoader component and use in filebrowser ([f1f2a05](https://github.com/owner/repo/commit/f1f2a05)) by @MordilloSan
* Fix terminal hang: add missing list_shells_main handler ([a782146](https://github.com/owner/repo/commit/a782146)) by @MordilloSan
* update ([bf42260](https://github.com/owner/repo/commit/bf42260)) by @MordilloSan
* - websocket and route channeling. ([be59e87](https://github.com/owner/repo/commit/be59e87)) by @MordilloSan
* code restructure ([4cbb450](https://github.com/owner/repo/commit/4cbb450)) by @MordilloSan
* Code restructure 2 ([ce3c881](https://github.com/owner/repo/commit/ce3c881)) by @MordilloSan
* code restructure 3 ([aac3b10](https://github.com/owner/repo/commit/aac3b10)) by @MordilloSan
* goroutine crash fix ([c533979](https://github.com/owner/repo/commit/c533979)) by @MordilloSan
* editor update ([34f63ac](https://github.com/owner/repo/commit/34f63ac)) by @MordilloSan
* bug fixes ([e9556fd](https://github.com/owner/repo/commit/e9556fd)) by @MordilloSan
* update all bug fix ([01f61ec](https://github.com/owner/repo/commit/01f61ec)) by @MordilloSan
* update ui fix ([4911d2c](https://github.com/owner/repo/commit/4911d2c)) by @MordilloSan
* zip compression feature ([9c38135](https://github.com/owner/repo/commit/9c38135)) by @MordilloSan
* Revert "feat: Implement streaming file upload and download via IPC" ([525af16](https://github.com/owner/repo/commit/525af16)) by @MordilloSan
* Refactor bridge handlers to use CallTypedWithSession for improved error handling and response parsing ([02878b4](https://github.com/owner/repo/commit/02878b4)) by @MordilloSan
* Refactor IPC Handler Functions to Support Streaming ([fff890e](https://github.com/owner/repo/commit/fff890e)) by @MordilloSan
* feature: ([bdd5dc6](https://github.com/owner/repo/commit/bdd5dc6)) by @MordilloSan
* linting update ([999da3d](https://github.com/owner/repo/commit/999da3d)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.9...v0.4.0


## v0.3.9 — 2025-10-26

### 🚀 Features

* feat: enhance logging in development mode and update go_logger dependency ([362be53](https://github.com/owner/repo/commit/362be53)) by @MordilloSan

### 🐛 Bug Fixes

* fix: package-lock.json ([6143300](https://github.com/owner/repo/commit/6143300)) by @MordilloSan
* fix: update log directory permissions to use octal notation ([ec398e3](https://github.com/owner/repo/commit/ec398e3)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.8...v0.3.9


## v0.3.8 — 2025-10-25

### 🚀 Features

* feat: add vite-plugin-compression for gzip support in build process ([04e345f](https://github.com/owner/repo/commit/04e345f)) by @MordilloSan
* feat: add vite-plugin-compression2 for enhanced gzip support in build process ([f6b4bda](https://github.com/owner/repo/commit/f6b4bda)) by @MordilloSan

### 🐛 Bug Fixes

* fix: remove frontend/package-lock.json from .gitignore ([1a7d6b5](https://github.com/owner/repo/commit/1a7d6b5)) by @MordilloSan

### ♻️ Refactoring

* refactor: update logger usage to structured logging across multiple files ([53bdd3f](https://github.com/owner/repo/commit/53bdd3f)) by @MordilloSan
* refactor: remove redundant log prefixes for clarity in update process ([562a0e9](https://github.com/owner/repo/commit/562a0e9)) by @MordilloSan

### 🏗️ Build

* build(deps-dev): bump vite from 7.1.9 to 7.1.11 in /frontend ([cfcddea](https://github.com/owner/repo/commit/cfcddea)) by @dependabot[bot]

### 🔄 Other Changes

* Refactor logging package usage across the backend ([31f28cb](https://github.com/owner/repo/commit/31f28cb)) by @MordilloSan
* Refactor logging in bridge handlers and update go_logger dependency ([71c41ec](https://github.com/owner/repo/commit/71c41ec)) by @MordilloSan
* Merge branch 'main' into dependabot/npm_and_yarn/frontend/vite-7.1.11 ([74fe917](https://github.com/owner/repo/commit/74fe917)) by @mordillo
* Merge pull request #39 from mordilloSan/dependabot/npm_and_yarn/frontend/vite-7.1.11 ([517cebc](https://github.com/owner/repo/commit/517cebc)) by @mordillo
* Merge branch 'main' into dev/v0.3.8 ([77f1d83](https://github.com/owner/repo/commit/77f1d83)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @dependabot[bot]
* @mordillo

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.7...v0.3.8


## v0.3.7 — 2025-10-25

### 🐛 Bug Fixes

* fix: update filebrowser ([670bf94](https://github.com/owner/repo/commit/670bf94)) by @MordilloSan

### 🔄 Other Changes

* filebrowser update ([668071c](https://github.com/owner/repo/commit/668071c)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.6...v0.3.7


## v0.3.6 — 2025-10-17

### 🐛 Bug Fixes

* fix: small dev bug ([d50c214](https://github.com/owner/repo/commit/d50c214)) by @MordilloSan
* fix: small bug ([fddf93a](https://github.com/owner/repo/commit/fddf93a)) by @MordilloSan
* fix: small bug ([a13be58](https://github.com/owner/repo/commit/a13be58)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.5...v0.3.6


## v0.3.5 — 2025-10-16

### 🐛 Bug Fixes

* fix: app update bug hunt ([20557b0](https://github.com/owner/repo/commit/20557b0)) by @MordilloSan
* fix: linting ([ba5f810](https://github.com/owner/repo/commit/ba5f810)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.4...v0.3.5


## v0.3.4 — 2025-10-16

### 🚀 Features

* feat: service table visual ([9722094](https://github.com/owner/repo/commit/9722094)) by @MordilloSan
* feat: share page start ([4ef014b](https://github.com/owner/repo/commit/4ef014b)) by @MordilloSan
* feat: circular gauge ([7928ed8](https://github.com/owner/repo/commit/7928ed8)) by @MordilloSan

### 🐛 Bug Fixes

* fix: obsolete circular gauge  deletion ([0469e9a](https://github.com/owner/repo/commit/0469e9a)) by @MordilloSan

### 💄 Style

* style: new circular gauge ([b8313bb](https://github.com/owner/repo/commit/b8313bb)) by @MordilloSan

### 🔄 Other Changes

* improv: card spacing tune ([062a462](https://github.com/owner/repo/commit/062a462)) by @MordilloSan
* linting ([e48ec3c](https://github.com/owner/repo/commit/e48ec3c)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.3...v0.3.4


## v0.3.2 — 2025-10-16

### 🚀 Features

* feat: filebrowser in dev mode ([f8aead9](https://github.com/owner/repo/commit/f8aead9)) by @MordilloSan

### 🐛 Bug Fixes

* fix: temperature report on components bug ([09f173c](https://github.com/owner/repo/commit/09f173c)) by @MordilloSan
* fix: dev mode non sudo login ([ddfb835](https://github.com/owner/repo/commit/ddfb835)) by @MordilloSan
* fix: outdated script ([4b0c56c](https://github.com/owner/repo/commit/4b0c56c)) by @MordilloSan
* fix: filebrowser docker container cleanup ([547de5d](https://github.com/owner/repo/commit/547de5d)) by @MordilloSan

### ⚡ Performance

* perf: theming update ([ab89e78](https://github.com/owner/repo/commit/ab89e78)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.1...v0.3.2


## v0.3.1 — 2025-10-15

### 🐛 Bug Fixes

* fix: update timeout ([d794980](https://github.com/owner/repo/commit/d794980)) by @MordilloSan
* fix: NIC now prefills values when setting manual option ([170f84b](https://github.com/owner/repo/commit/170f84b)) by @MordilloSan
* fix: linting ([db80d24](https://github.com/owner/repo/commit/db80d24)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.3.0...v0.3.1


## v0.3.0 — 2025-10-15

### 🚀 Features

* feat: big service page update with action functional ([977e717](https://github.com/owner/repo/commit/977e717)) by @MordilloSan

### 🐛 Bug Fixes

* fix: linting ([738d862](https://github.com/owner/repo/commit/738d862)) by @MordilloSan
* fix:code cleaning ([47b9624](https://github.com/owner/repo/commit/47b9624)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.33...v0.3.0


## v0.2.31 — 2025-10-15

### 🔄 Other Changes

* readme update ([d523487](https://github.com/owner/repo/commit/d523487)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.30...v0.2.31


## v0.2.30 — 2025-10-15

### 🐛 Bug Fixes

* fix: app install bug ([692be78](https://github.com/owner/repo/commit/692be78)) by @MordilloSan

### 🔄 Other Changes

* up ([3be5b3d](https://github.com/owner/repo/commit/3be5b3d)) by @MordilloSan
* script update ([db190f6](https://github.com/owner/repo/commit/db190f6)) by @MordilloSan
* update ([a4c1c9a](https://github.com/owner/repo/commit/a4c1c9a)) by @MordilloSan
* update ([b6c1ce6](https://github.com/owner/repo/commit/b6c1ce6)) by @MordilloSan
* update ([5456cb8](https://github.com/owner/repo/commit/5456cb8)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.29...v0.2.30


## v0.2.29 — 2025-10-15

### 🐛 Bug Fixes

* fix: debug and test for app update ([409c297](https://github.com/owner/repo/commit/409c297)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.28...v0.2.29


## v0.2.28 — 2025-10-15

### 🐛 Bug Fixes

* fix: readme typo ([35437dd](https://github.com/owner/repo/commit/35437dd)) by @MordilloSan
* fix: update debug ([cfb8eca](https://github.com/owner/repo/commit/cfb8eca)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.27...v0.2.28


## v0.2.27 — 2025-10-15

### 🐛 Bug Fixes

* fix: script path fix ([f894c2c](https://github.com/owner/repo/commit/f894c2c)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.26...v0.2.27


## v0.2.26 — 2025-10-15

### 🐛 Bug Fixes

* fix: app update bug ([ff05a09](https://github.com/owner/repo/commit/ff05a09)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.25...v0.2.26


## v0.2.25 — 2025-10-15

### 🚀 Features

* feat: Footer Versioning fix: coherence of axios and react query ([688e6a4](https://github.com/owner/repo/commit/688e6a4)) by @MordilloSan

### 🐛 Bug Fixes

* fix: linting ([51219db](https://github.com/owner/repo/commit/51219db)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.24...v0.2.25


## v0.2.24 — 2025-10-15

### 🐛 Bug Fixes

* fix: update script ([375c008](https://github.com/owner/repo/commit/375c008)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.23...v0.2.24


## v0.2.23 — 2025-10-15

### 🔄 Other Changes

* debug: app update script bug ([6ec2a6d](https://github.com/owner/repo/commit/6ec2a6d)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.22...v0.2.23


## v0.2.22 — 2025-10-15

### 🚀 Features

* feat: changelog workflow ([cd66905](https://github.com/owner/repo/commit/cd66905)) by @MordilloSan
* feat: changelog improvement ([a6bf61e](https://github.com/owner/repo/commit/a6bf61e)) by @MordilloSan
* feat: go mod tidy ([fd7e18c](https://github.com/owner/repo/commit/fd7e18c)) by @MordilloSan

### 🐛 Bug Fixes

* fix: changelog bugfix ([c40678e](https://github.com/owner/repo/commit/c40678e)) by @MordilloSan
* fix: changelog fix ([4f4ec0c](https://github.com/owner/repo/commit/4f4ec0c)) by @MordilloSan

### 🔄 Other Changes

* app update bug fix ([4cd0e2f](https://github.com/owner/repo/commit/4cd0e2f)) by @MordilloSan
* changelog improvement ([3ca6a26](https://github.com/owner/repo/commit/3ca6a26)) by @MordilloSan
* dependencie updates ([7d7d499](https://github.com/owner/repo/commit/7d7d499)) by @MordilloSan
* makefile update ([85077dd](https://github.com/owner/repo/commit/85077dd)) by @MordilloSan
* changelog fix ([cc6e6a6](https://github.com/owner/repo/commit/cc6e6a6)) by @MordilloSan
* fix makefile bug ([371d714](https://github.com/owner/repo/commit/371d714)) by @MordilloSan

### 👥 Contributors

* @MordilloSan

**Full Changelog**: https://github.com/owner/repo/compare/v0.2.21...v0.2.22


## v0.2.21 — 2025-10-14

### 🔄 Other Changes

* go reportcard readme update ([1d18b5e](https://github.com/owner/repo/commit/1d18b5e)) by @MordilloSan
* testing workflow and general linting ([0456cce](https://github.com/owner/repo/commit/0456cce)) by @MordilloSan
* app update fix attempt ([9c49eed](https://github.com/owner/repo/commit/9c49eed)) by @MordilloSan
* readme update ([af60e75](https://github.com/owner/repo/commit/af60e75)) by @MordilloSan
* linting ([d441960](https://github.com/owner/repo/commit/d441960)) by @MordilloSan
* Merge pull request #19 from mordilloSan/dev/v0.2.21 ([3ee685b](https://github.com/owner/repo/commit/3ee685b)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.20...v0.2.21

## v0.2.20 — 2025-10-14

### 🔄 Other Changes

* Atualizar o go.mod ([9ca7f8e](https://github.com/owner/repo/commit/9ca7f8e)) by @mordillo
* go path fix ([01c0a4f](https://github.com/owner/repo/commit/01c0a4f)) by @MordilloSan
* go path bug fix ([ce91a24](https://github.com/owner/repo/commit/ce91a24)) by @MordilloSan
* Merge pull request #18 from mordilloSan/dev/v0.2.20 ([ae8459d](https://github.com/owner/repo/commit/ae8459d)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.16...v0.2.20

## v0.2.16 — 2025-10-14

### 🔄 Other Changes

* app update bugfix ([58bbe18](https://github.com/owner/repo/commit/58bbe18)) by @MordilloSan
* file permissions bug ([6312ef3](https://github.com/owner/repo/commit/6312ef3)) by @MordilloSan
* Merge pull request #17 from mordilloSan/dev/v0.2.16 ([1e6dc95](https://github.com/owner/repo/commit/1e6dc95)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.15...v0.2.16

## v0.2.15 — 2025-10-14

### 🔄 Other Changes

* makefile merge workflow update ([7755ee9](https://github.com/owner/repo/commit/7755ee9)) by @MordilloSan
* toast duration fix ([03ebea8](https://github.com/owner/repo/commit/03ebea8)) by @MordilloSan
* Merge pull request #16 from mordilloSan/dev/v0.2.15 ([1295494](https://github.com/owner/repo/commit/1295494)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.14...v0.2.15

## v0.2.14 — 2025-10-14

### 🔄 Other Changes

* app autoupdate ([55d7168](https://github.com/owner/repo/commit/55d7168)) by @MordilloSan
* Merge pull request #15 from mordilloSan/dev/v0.2.14 ([e3f37a7](https://github.com/owner/repo/commit/e3f37a7)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.13...v0.2.14

## v0.2.13 — 2025-10-14

### 🔄 Other Changes

* update banner ([65baf24](https://github.com/owner/repo/commit/65baf24)) by @MordilloSan
* makefile update ([a410fd3](https://github.com/owner/repo/commit/a410fd3)) by @MordilloSan
* makefile update ([917e569](https://github.com/owner/repo/commit/917e569)) by @MordilloSan
* Merge pull request #13 from mordilloSan/dev/v0.2.12 ([0203e18](https://github.com/owner/repo/commit/0203e18)) by @mordillo
* makefile update ([e1c9825](https://github.com/owner/repo/commit/e1c9825)) by @MordilloSan
* makefile update ([ee1d3d0](https://github.com/owner/repo/commit/ee1d3d0)) by @MordilloSan
* Merge pull request #14 from mordilloSan/dev/v0.2.13 ([fe97083](https://github.com/owner/repo/commit/fe97083)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.12...v0.2.13

## v0.2.12 — 2025-10-14

### 🔄 Other Changes

* Bump github.com/quic-go/quic-go from 0.54.0 to 0.54.1 in /backend ([c58abf4](https://github.com/owner/repo/commit/c58abf4)) by @dependabot[bot]
* Merge branch 'main' into dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1 ([93d0426](https://github.com/owner/repo/commit/93d0426)) by @mordillo
* Merge remote-tracking branch 'origin/dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1' into dev/v0.2.12 ([acf2e52](https://github.com/owner/repo/commit/acf2e52)) by @MordilloSan
* Merge branch 'main' into dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1 ([e33c26b](https://github.com/owner/repo/commit/e33c26b)) by @mordillo
* Merge pull request #6 from mordilloSan/dependabot/go_modules/backend/github.com/quic-go/quic-go-0.54.1 ([ce141c8](https://github.com/owner/repo/commit/ce141c8)) by @mordillo
* install process and makefile optimization ([b9cdf2d](https://github.com/owner/repo/commit/b9cdf2d)) by @MordilloSan
* makefile update ([d226403](https://github.com/owner/repo/commit/d226403)) by @MordilloSan
* makefile upgrade ([9c1f218](https://github.com/owner/repo/commit/9c1f218)) by @MordilloSan
* makefile upgrade ([e484b97](https://github.com/owner/repo/commit/e484b97)) by @MordilloSan
* makefile upgrade ([e91edb8](https://github.com/owner/repo/commit/e91edb8)) by @MordilloSan
* makefile upgrade ([0ba88c0](https://github.com/owner/repo/commit/0ba88c0)) by @MordilloSan
* up ([ed13d0b](https://github.com/owner/repo/commit/ed13d0b)) by @MordilloSan
* update ([76e32ec](https://github.com/owner/repo/commit/76e32ec)) by @MordilloSan
* make update ([5a22e8f](https://github.com/owner/repo/commit/5a22e8f)) by @MordilloSan
* bug fix ([b69ceb9](https://github.com/owner/repo/commit/b69ceb9)) by @MordilloSan
* bug fix ([cdadb91](https://github.com/owner/repo/commit/cdadb91)) by @MordilloSan
* test ([fcec6db](https://github.com/owner/repo/commit/fcec6db)) by @MordilloSan
* bug fix ([2bc290f](https://github.com/owner/repo/commit/2bc290f)) by @MordilloSan
* up ([a87101d](https://github.com/owner/repo/commit/a87101d)) by @MordilloSan
* up ([d76e08b](https://github.com/owner/repo/commit/d76e08b)) by @MordilloSan
* bug fix ([9fc5a78](https://github.com/owner/repo/commit/9fc5a78)) by @MordilloSan
* bug fix ([29bfb26](https://github.com/owner/repo/commit/29bfb26)) by @MordilloSan
* bug fix ([981bacc](https://github.com/owner/repo/commit/981bacc)) by @MordilloSan
* Merge pull request #12 from mordilloSan/dev/v0.2.12 ([2ec37b3](https://github.com/owner/repo/commit/2ec37b3)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @dependabot[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.11...v0.2.12

## v0.2.11 — 2025-10-13

### 🔄 Other Changes

* updatebanner bug ([26d0e10](https://github.com/owner/repo/commit/26d0e10)) by @MordilloSan
* Merge pull request #11 from mordilloSan/dev/v0.2.11 ([8e4ff6e](https://github.com/owner/repo/commit/8e4ff6e)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.10...v0.2.11

## v0.2.10 — 2025-10-13

### 🔄 Other Changes

* version API ([8b7add0](https://github.com/owner/repo/commit/8b7add0)) by @MordilloSan
* Merge pull request #10 from mordilloSan/dev/v0.2.10 ([38b6c7f](https://github.com/owner/repo/commit/38b6c7f)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.9...v0.2.10

## v0.2.9 — 2025-10-13

### 🔄 Other Changes

* app update procedure ([49c3ef1](https://github.com/owner/repo/commit/49c3ef1)) by @MordilloSan
* linting fix ([c05e15d](https://github.com/owner/repo/commit/c05e15d)) by @MordilloSan
* Merge pull request #9 from mordilloSan/dev/v0.2.9 ([6471ea3](https://github.com/owner/repo/commit/6471ea3)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.8...v0.2.9

## v0.2.8 — 2025-10-13

### 🔄 Other Changes

* automatic updates ([61ccf00](https://github.com/owner/repo/commit/61ccf00)) by @MordilloSan
* linting ([6048b8d](https://github.com/owner/repo/commit/6048b8d)) by @MordilloSan
* Merge pull request #8 from mordilloSan/dev/v0.2.8 ([5fc9044](https://github.com/owner/repo/commit/5fc9044)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.6...v0.2.8

## v0.2.6 — 2025-10-11

### 📚 Documentation

* docs: update changelog for v0.2.6 ([8e4829a](https://github.com/owner/repo/commit/8e4829a)) by @MordilloSan
* docs: update changelog for v0.2.6 ([c7c95b9](https://github.com/owner/repo/commit/c7c95b9)) by @MordilloSan

### 🔄 Other Changes

* versioning update ([84590ec](https://github.com/owner/repo/commit/84590ec)) by @MordilloSan
* websocket ([6f0f9e0](https://github.com/owner/repo/commit/6f0f9e0)) by @MordilloSan
* env cleanup ([4305bbe](https://github.com/owner/repo/commit/4305bbe)) by @MordilloSan
* env bug fix ([8020e12](https://github.com/owner/repo/commit/8020e12)) by @MordilloSan
* env bug fix ([b0d34c6](https://github.com/owner/repo/commit/b0d34c6)) by @MordilloSan
* socket determination update enviorment variables removal C helper update ([0c9c973](https://github.com/owner/repo/commit/0c9c973)) by @MordilloSan
* changelog update ([8dedd7c](https://github.com/owner/repo/commit/8dedd7c)) by @MordilloSan
* changelog update ([25e114f](https://github.com/owner/repo/commit/25e114f)) by @MordilloSan
* makefile changelog code ([7bc5046](https://github.com/owner/repo/commit/7bc5046)) by @MordilloSan
* makefile bugfix ([bbcb515](https://github.com/owner/repo/commit/bbcb515)) by @MordilloSan
* makefile improvement ([0bf2ad8](https://github.com/owner/repo/commit/0bf2ad8)) by @MordilloSan
* makefile update ([c49e945](https://github.com/owner/repo/commit/c49e945)) by @MordilloSan
* changelog update ([1ac238f](https://github.com/owner/repo/commit/1ac238f)) by @MordilloSan
* pull request workflow ([a6c0382](https://github.com/owner/repo/commit/a6c0382)) by @MordilloSan
* Merge pull request #7 from mordilloSan/dev/v0.2.6 ([f2a54d7](https://github.com/owner/repo/commit/f2a54d7)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.5...v0.2.6

## v0.2.5 — 2025-10-09

### 🔄 Other Changes

* package updater refreshed ([0bb7a92](https://github.com/owner/repo/commit/0bb7a92)) by @MordilloSan
* Merge pull request #5 from mordilloSan/dev/v0.2.5 ([a530b05](https://github.com/owner/repo/commit/a530b05)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.3...v0.2.5

## v0.2.3 — 2025-10-08

### 🔄 Other Changes

* testing update ([a26ffb0](https://github.com/owner/repo/commit/a26ffb0)) by @MordilloSan
* golinting update ([304d5f8](https://github.com/owner/repo/commit/304d5f8)) by @MordilloSan
* github workflow update ([8eb8383](https://github.com/owner/repo/commit/8eb8383)) by @MordilloSan
* linting update ([052a9d1](https://github.com/owner/repo/commit/052a9d1)) by @MordilloSan
* Merge pull request #4 from mordilloSan/dev/v0.2.3 ([a677459](https://github.com/owner/repo/commit/a677459)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.2...v0.2.3

## v0.2.2 — 2025-10-08

### 🔄 Other Changes

* pullrequest testing workflow update ([462bf3c](https://github.com/owner/repo/commit/462bf3c)) by @MordilloSan
* makefile bugfix ([c96f47c](https://github.com/owner/repo/commit/c96f47c)) by @MordilloSan
* test workflow ([06d95fe](https://github.com/owner/repo/commit/06d95fe)) by @MordilloSan
* update to the workflow ([6baed35](https://github.com/owner/repo/commit/6baed35)) by @MordilloSan
* Merge pull request #3 from mordilloSan/dev/v0.2.2 ([40bcd3a](https://github.com/owner/repo/commit/40bcd3a)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.1...v0.2.2

## v0.2.1 — 2025-10-08

### 🔄 Other Changes

* codeql unit conversion fixes ([5a6b011](https://github.com/owner/repo/commit/5a6b011)) by @MordilloSan
* Merge pull request #2 from mordilloSan/dev/v0.2.1 ([3af2818](https://github.com/owner/repo/commit/3af2818)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.2.0...v0.2.1

## v0.2.0 — 2025-10-08

### 🐛 Bug Fixes

* fix(ci): exclude bot commits from changelog generation ([b870c42](https://github.com/owner/repo/commit/b870c42)) by @MordilloSan

### 🔄 Other Changes

* Update to the changelog workflow ([d9bac52](https://github.com/owner/repo/commit/d9bac52)) by @MordilloSan
* readme update ([857bc16](https://github.com/owner/repo/commit/857bc16)) by @MordilloSan
* makefile update ([5ff8ee6](https://github.com/owner/repo/commit/5ff8ee6)) by @MordilloSan
* makefile bugfix ([9d41405](https://github.com/owner/repo/commit/9d41405)) by @MordilloSan
* Merge pull request #1 from mordilloSan/dev/v0.2.0 ([53a87b5](https://github.com/owner/repo/commit/53a87b5)) by @mordillo

### 👥 Contributors

* @MordilloSan
* @github-actions[bot]
* @mordillo


**Full Changelog**: https://github.com/owner/repo/compare/v0.1.0...v0.2.0

## v0.1.0 — 2025-10-08

### 🔄 Other Changes

* Initial commit - LinuxIO v0.1.0 ([a8180e2](https://github.com/owner/repo/commit/a8180e2)) by @MordilloSan
* update ([a6f3cab](https://github.com/owner/repo/commit/a6f3cab)) by @MordilloSan
* update ([5712112](https://github.com/owner/repo/commit/5712112)) by @MordilloSan
* update ([de4f213](https://github.com/owner/repo/commit/de4f213)) by @MordilloSan

### 👥 Contributors

* @MordilloSan


**Full Changelog**: https://github.com/owner/repo/compare/...v0.1.0
