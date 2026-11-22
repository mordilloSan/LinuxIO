# Third-party notices for what LinuxIO ships

## Status

Not built, waiting on a choice between the options below. Measured 2026-11-21 from a build's programs in `build/`, the Go module cache and `frontend/package-lock.json`.

## The gap

[Third-Party Notices](../THIRD_PARTY_NOTICES.md) names six projects: three LinuxIO adapted code from (FileBrowser Quantum, Beszel, lot-map) and three Go modules (go-sqlite3, goccy/go-yaml, golang.org/x/sys). A release ships far more. MIT, BSD, Apache-2.0, MPL-2.0 and OFL-1.1 each ask for their notice to go with every binary copy.

- **Go.** The eight Go programs link 50 third-party modules. `go.mod` lists 61; 11 never reach a shipped program.
  - Licences: 27 Apache-2.0, 13 MIT, 6 BSD-3, 2 BSD-2, 1 MPL-2.0 (`libp2p/go-yamux/v5`). `jaypipes/ghw` is Apache-2.0 in a file named `COPYING`.
  - `gopkg.in/yaml.v3` carries a `NOTICE` file, which Apache-2.0 §4(d) asks to be reproduced.
  - The Go standard library and runtime (BSD-3, The Go Authors) are in every program and are not named. The x/sys entry has the same text but names only x/sys.
- **Frontend.** The bundle is embedded in `linuxio-webserver` (`backend/webserver/web/frontend.go`) and sent to every browser. The lockfile's production closure is 167 packages, an upper bound on what Vite bundles:
  - 159 MIT;
  - ISC, 0BSD, BSD-3 (`diff`), Apache-2.0 (`@agentclientprotocol/sdk`) and Unlicense (`isbot`);
  - MPL-2.0 (`@novnc/novnc`);
  - OFL-1.1 (`@fontsource-variable/inter`, `@fontsource/jetbrains-mono`), whose font files are bundled.
- **Icons.** `frontend/scripts/generate-icons.mjs` copies icons from six devDependency sets into `frontend/src/icons/icons.ts`: bi, eos-icons and ph (MIT), la and mdi (Apache-2.0), simple-icons (CC0-1.0). The data sits in LinuxIO's own source, so neither Vite nor a collector over production packages sees it.
- **MPL-2.0** (`libp2p/go-yamux/v5`, `@novnc/novnc`). §3.2 also asks a binary distribution to tell recipients where the source of those files is. Each needs a source link and its version, not only the licence text.
- `linuxio-auth` links libpam, libsystemd and libc from the host at run time and ships no third-party code.

## Who reads the notices file

A fix that misses one of these leaves it with the short file:

- **The packages.** `packaging/scripts/package-deb.sh` and `packaging/scripts/package-rpm.sh` take `docs/THIRD_PARTY_NOTICES.md` from the folder beside `packaging/`, and the DEB and the RPM install it in `/usr/share/doc/linuxio/`, beside `copyright` or as `%doc` ([Packages](../packaging.md)). A release's assets are the two packages, so they are the only way the file reaches a host.
- **The test VM.** `packaging/vm/linuxio-vm.sh` copies it from the checkout for those scripts.
- `packaging/scripts/tests/package-files-fixtures.sh` checks that it lives under `docs/`, and `packaging/scripts/tests/package-contents.sh` that both packages hold it.

## Tools already at hand

- Vite 8's `build.license` option writes the full licence text of every package it bundles, and only those. It is off in `frontend/config/vite.config.ts`. It reads `LICENSE` and `LICENCE` files, not `NOTICE`; no bundled package has one today.
- Every Go program records its exact modules and versions (`go version -m`), and building them puts their licence files in the module cache.

## Options

### A. A generator step in `release.yml`

The release's compile job runs go-licenses and an npm licence collector and appends their output to the notices before it assembles the packages.

- Size: one file. Adds two third-party tools to the job that builds what a release ships.
- Covers a release's packages only. `make localinstall` and the test VMs keep the short file.
- Misses the Go standard library, the icon sets and the MPL source links.
- go-licenses reads the package graph through `go list` under the GOOS, build tags and CGO setting it runs with, not what the programs linked.
- A failure first shows when a release is cut. Pull-request CI does not run it.

### B. Generate the notices beside the programs (recommended)

The build writes `build/THIRD_PARTY_NOTICES.md` from what it built, and everything that ships the file reads it from there.

- Turn on `build.license` in `vite.config.ts`.
- A new `packaging/scripts/third-party-notices.sh` (about 60 lines) with a fixture test. It:
  - reads each Go program's modules with `go version -m`;
  - copies each module's `LICENSE`, `COPYING` and `NOTICE` from the module cache, the Apache-2.0 text once, since `LICENSE` already is that text;
  - appends Vite's file and the hand-written `docs/THIRD_PARTY_NOTICES.md`;
  - fails when a linked module has no licence file, so a new dependency cannot arrive unnoticed.
- A Make target that `build-binaries` runs.
- One-line path changes in `package-deb.sh`, `package-rpm.sh`, `linuxio-vm.sh` and `package-files-fixtures.sh`, so the packages take the file from the build folder.
- `docs/THIRD_PARTY_NOTICES.md` keeps FileBrowser Quantum, Beszel and lot-map, adds Go, the icon sets and the MPL source links, and drops go-sqlite3, go-yaml and x/sys, which the script then writes.
- The README line and the docs that cover packaging and the release.
- Size: about ten files. No new tool and no new privileged or public surface. The installed path stays as it is.

Vite writes its file into the build's output folder, which is embedded in the webserver, so the file is also served at a URL. That is harmless, and fits code sent to browsers.

### C. Generate the notices and commit them

The script from B writes `docs/THIRD_PARTY_NOTICES.md`, the file is committed, and a CI check fails when it is stale.

- Everything that reads `docs/` keeps working, and licence changes show in review diffs.
- The check needs a full Go and frontend build to know what is linked, which pull-request CI does not do today.
- Every Dependabot bump fails the check until someone regenerates the file, unless the headings leave versions out.
- Adds a committed file of about 5,000 lines.
- Size: about six files and a CI job.

## Recommendation

B. It is the only option that reaches every reader above, works from what was actually linked, and adds no tool. A is the cheapest, but it leaves `make localinstall`, the test VMs, Go itself, the icon sets and the MPL source links uncovered.

## Measuring again

- Go: `go version -m build/linuxio-*` lists each program's modules. Their licence files are under `$(go env GOMODCACHE)`, with each capital letter in a path written as `!` and the lower-case letter.
- Frontend: the `package-lock.json` entries without `"dev": true` give the upper bound. `build.license` gives the exact set.
