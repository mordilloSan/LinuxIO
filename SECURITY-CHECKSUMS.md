# Security: Checksum Verification

This document describes LinuxIO's multi-layer checksum verification system for protecting against supply-chain attacks.

## Architecture Overview

LinuxIO implements two-layer cryptographic verification:

1. **Install Script Verification** (by Go updater)
   - Go code downloads install script + checksum from GitHub Release
   - Verifies SHA256 checksum before execution
   - Script executes only if checksum matches

2. **Binary Verification** (by install script)
   - Install script downloads binaries + SHA256SUMS from GitHub Release
   - Verifies checksums for all binaries before installation
   - Binaries install only if all checksums match

## Threat Model

### Attacks Mitigated

- **Man-in-the-middle (MITM)**: Checksums prevent tampered downloads
- **Compromised CDN**: Modified files fail checksum verification
- **Malicious releases**: Only signed releases from GitHub work
- **Script tampering**: Install script verified before execution

### Trust Assumptions

- GitHub's release infrastructure is trusted
- Release workflow runs in GitHub's trusted environment
- Private keys for signing releases remain secure
- HTTPS/TLS protects transport layer

## Verification Flow

### Update Initiated

```
User → linuxio-webserver → linuxio-bridge (privileged) → performUpdate()
```

### Download & Verify Install Script

```go
// 1. Build URLs for specific release version
scriptURL, checksumURL := buildScriptURLs(version)
// Example: https://github.com/mordilloSan/LinuxIO/releases/download/v0.7.0/install-linuxio-binaries.sh
//          https://github.com/mordilloSan/LinuxIO/releases/download/v0.7.0/install-linuxio-binaries.sh.sha256

// 2. Download checksum file
expectedChecksum := downloadChecksum(checksumURL)

// 3. Download install script
scriptBytes := downloadScript(scriptURL)

// 4. Verify integrity
actualChecksum := computeSHA256(scriptBytes)
if actualChecksum != expectedChecksum {
    ABORT: "checksum verification failed: script integrity compromised"
}

// 5. Execute verified script via systemd-run
systemd-run --pipe /bin/bash -s -- <version> < verified_script
```

**Code Location**: [backend/bridge/handlers/control/control.go:148-240](backend/bridge/handlers/control/control.go#L148-L240)

### Install Script Verifies Binaries

The verified install script then:

```bash
# Download binaries and checksums from same release
wget "https://github.com/mordilloSan/LinuxIO/releases/download/${VERSION}/linuxio"
wget "https://github.com/mordilloSan/LinuxIO/releases/download/${VERSION}/SHA256SUMS"

# Verify all binaries
sha256sum -c SHA256SUMS || {
    echo "ERROR: Binary checksum verification failed"
    exit 1
}

# Install only after verification
install -m 0755 linuxio /usr/local/bin/
```

## Release Workflow

### Automated Checksum Generation

When a release is created (merge dev/v* → main or manual dispatch):

```yaml
# .github/workflows/release.yml

- name: Build all binaries
  run: |
    make build-vite
    make build-bridge
    make build-backend
    make build-auth
    make build-cli

- name: Prepare install script for release
  run: |
    cp packaging/scripts/install-linuxio-binaries.sh .
    sha256sum install-linuxio-binaries.sh > install-linuxio-binaries.sh.sha256

- name: Generate checksums
  run: |
    sha256sum linuxio linuxio-webserver linuxio-bridge linuxio-auth *.tar.gz > SHA256SUMS

- name: Create draft release
  with:
    artifacts: |
      linuxio-v0.7.0-linux-amd64.tar.gz
      linuxio
      linuxio-webserver
      linuxio-bridge
      linuxio-auth
      SHA256SUMS
      install-linuxio-binaries.sh
      install-linuxio-binaries.sh.sha256
```

**Workflow Location**: [.github/workflows/release.yml:90-251](.github/workflows/release.yml#L90-L251)

### Release Artifacts

Each release includes:

- **Binaries**: `linuxio`, `linuxio-webserver`, `linuxio-bridge`, `linuxio-auth`
- **Tarball**: `linuxio-v0.7.0-linux-amd64.tar.gz`
- **Binary Checksums**: `SHA256SUMS` (checksums for all binaries + tarball)
- **Install Script**: `install-linuxio-binaries.sh`
- **Script Checksum**: `install-linuxio-binaries.sh.sha256`

## Version Consistency

### Release-Based Downloads

All downloads use **release tags** for version consistency:

```
✅ CORRECT: https://github.com/.../releases/download/v0.7.0/install-linuxio-binaries.sh
❌ WRONG:   https://raw.githubusercontent.com/.../main/packaging/scripts/install-linuxio-binaries.sh
```

**Why?** Downloading from `main` branch gets unreleased code that may not match the binaries being installed. Release-based downloads ensure the script version matches the binary version.

### Version Matching

```
User requests: v0.7.0
  ↓
Download: v0.7.0/install-linuxio-binaries.sh
  ↓
Script downloads: v0.7.0/linuxio binaries
  ↓
Result: Script version = Binary version ✅
```

## Developer Workflow

### Making Script Changes

1. **Edit the install script**:
   ```bash
   vim packaging/scripts/install-linuxio-binaries.sh
   ```

2. **Test locally** (optional):
   ```bash
   bash packaging/scripts/install-linuxio-binaries.sh v0.7.0
   ```

3. **Commit changes**:
   ```bash
   git add packaging/scripts/install-linuxio-binaries.sh
   git commit -m "fix: update install script error handling"
   ```

4. **Checksum generation happens automatically** during release workflow
   - No manual checksum updates needed
   - Checksums generated at release time
   - Included automatically in GitHub Release

### Creating a Release

1. **Merge dev/vX.Y.Z → main** (or manual dispatch)
2. **GitHub Actions automatically**:
   - Builds all binaries
   - Copies install script to release directory
   - Generates `install-linuxio-binaries.sh.sha256`
   - Generates `SHA256SUMS` for all binaries
   - Creates GitHub Release with all artifacts
   - Publishes release

3. **Updater uses release artifacts**:
   - Downloads from release tag (not main branch)
   - Verifies checksums
   - Installs verified binaries

## Security Testing

### Verify Checksum Generation

```bash
# Check release has all required files
gh release view v0.7.0 --json assets -q '.assets[].name'

# Expected output:
# linuxio-v0.7.0-linux-amd64.tar.gz
# linuxio
# linuxio-webserver
# linuxio-bridge
# linuxio-auth
# SHA256SUMS
# install-linuxio-binaries.sh
# install-linuxio-binaries.sh.sha256
```

### Test Checksum Verification

```bash
# Download and verify script checksum
wget https://github.com/mordilloSan/LinuxIO/releases/download/v0.7.0/install-linuxio-binaries.sh
wget https://github.com/mordilloSan/LinuxIO/releases/download/v0.7.0/install-linuxio-binaries.sh.sha256

sha256sum -c install-linuxio-binaries.sh.sha256
# Expected: install-linuxio-binaries.sh: OK
```

### Simulate Tampering

```bash
# Tamper with script
echo "malicious code" >> install-linuxio-binaries.sh

# Verify detection
sha256sum -c install-linuxio-binaries.sh.sha256
# Expected: install-linuxio-binaries.sh: FAILED
```

### Test Go Updater

```bash
# Trigger update via API
curl -X POST http://localhost:8080/api/system/update \
  -H "Content-Type: application/json" \
  -d '{"version": "v0.7.0"}'

# Check logs for verification
journalctl -u linuxio -f | grep -i checksum
# Expected:
# expected checksum: abc123...
# computed checksum: abc123...
# checksum verified successfully
```

## Limitations

### Not Protected

- **Compromised GitHub Account**: Attacker with repo access can create malicious releases
- **Compromised GitHub Actions**: Malicious workflow can generate valid checksums for bad binaries
- **Initial Installation**: First install trusts GitHub Release (no prior checksum to verify against)
- **Local File Tampering**: After installation, files can be modified locally

### Future Improvements

- **GPG Signed Releases**: Add cryptographic signatures to releases
- **Binary Signing**: Sign binaries with code signing certificate
- **Reproducible Builds**: Allow independent verification of build artifacts
- **Checksum Pinning**: Remember checksums from first install for subsequent updates

## References

- Install Script: [packaging/scripts/install-linuxio-binaries.sh](packaging/scripts/install-linuxio-binaries.sh)
- Go Updater: [backend/bridge/handlers/control/control.go](backend/bridge/handlers/control/control.go)
- Release Workflow: [.github/workflows/release.yml](.github/workflows/release.yml)
- Makefile Helpers: [Makefile](Makefile)
