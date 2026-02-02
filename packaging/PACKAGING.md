# LinuxIO Packaging Guide

This guide covers building LinuxIO .deb and .rpm packages from the repository.

## Overview

- Packages are built from prebuilt binaries and the assets under `packaging/`.
- Binaries are installed to `/usr/local/bin` to match the runtime paths in the code.
- Systemd units are installed to `/usr/lib/systemd/system`.
- Config files are installed under `/etc` and marked as config (noreplace / conffiles).

## Versioning

The package version is derived from `GIT_VERSION` (same value used by the Makefile):

- Release tag `v1.2.3` -> Debian `1.2.3`, RPM `Version=1.2.3 Release=1`
- Dev build `dev-0.6.12` -> Debian `0.6.12~dev`, RPM `Version=0.6.12 Release=0.dev.<sha>`
- If the base version does not start with a digit (e.g., a raw commit hash), the package version falls back to `0.0.0`.

You can override:

```bash
GIT_VERSION=v1.2.3 GIT_COMMIT_SHORT=abc123 make build-deb
GIT_VERSION=v1.2.3 GIT_COMMIT_SHORT=abc123 make build-rpm
```

## Build Commands

```bash
make build-deb
make build-rpm
make build-packages
```

By default the targets build binaries first. To skip rebuilding (e.g., in CI):

```bash
make build-deb PKG_SKIP_BUILD=1
make build-rpm PKG_SKIP_BUILD=1
```

Artifacts are written to `dist/`.

## Requirements

- Debian/Ubuntu: `dpkg-deb`
- RHEL/Fedora: `rpmbuild` (rpm-build)

## Architecture

LinuxIO packages are built for `amd64` only.
