# Native Docker Update Engine Plan

Status: implementation complete; Batches 1 through 4 are in the tree. Real
Docker/Compose integration coverage and runtime upgrade validation remain
release gates.

## Goal

Own image update discovery and application inside LinuxIO without embedding or
forking Watchtower. Use the Docker Engine API for registry metadata, image
pulls, container inspection, and standalone-container replacement. Use Docker
Compose for Compose projects so project-level configuration remains the source
of truth.

The persistent update-status document remains the read model for the UI. It
stores one observation per running container, including the local and remote
manifest digests when available.

## Ownership model

### Compose-managed containers

A container with the standard Compose project and service labels is not an
independent update unit. LinuxIO resolves the project name, working directory,
config files, environment files, and service set, then reconciles the project
through Docker Compose. It must not reconstruct those containers from inspect
data.

### Standalone containers

Standalone updates are explicit opt-in operations. LinuxIO captures the
container's creation configuration and host configuration, pulls the requested
tag, stops and renames the old container, creates the replacement under the
original name, starts it, and verifies it. Failure restores the old container;
the old image and rollback metadata remain until success is confirmed.

Unsupported or unsafe configurations are reported, not guessed.

The supported standalone path requires a running, stable container with
recreatable inspect data. It preserves runtime configuration, named and
anonymous volumes, and portable network endpoint settings. Auto-remove,
container-ID files, Swarm ownership, static addressing, and containers that
provide another container's network namespace or volumes are refused before
the image pull or any container mutation.

### Image checking

LinuxIO asks the Docker daemon for the current local image metadata and for the
registry distribution descriptor of the configured image reference. Registry
access is intentionally anonymous because LinuxIO currently targets public
images. A movable tag has an update when the remote manifest digest is absent
from the local image's repository digests. Digest-pinned and image-ID references
are immutable and never report an update.

## Delivery batches

### Batch 1: native update checking

- [x] Replace Watchtower monitor-only runs with Docker Engine
  `DistributionInspect`.
- [x] Normalize image names using the distribution reference library.
- [x] Keep registry access anonymous; authenticated private registries are out
  of scope.
- [x] Deduplicate registry requests for containers sharing an image reference
  and local image.
- [x] Isolate per-image failures while preserving request cancellation.
- [x] Persist local and remote manifest digests for diagnostics.
- [x] Cover current, stale, pinned, local-only, duplicate, anonymous, and failed
  checks in unit tests.

### Batch 2: Compose project updates

- [x] Resolve every selected Compose container to its project and service.
- [x] Coalesce scheduled services by project and reject incomplete metadata.
- [x] Pull selected services, then reconcile them with Docker Compose using the
  project's recorded configuration.
- [x] Record per-container outcomes and refresh status only after reconciliation.
- [ ] Add integration coverage against a real Docker daemon and Compose plugin.

### Batch 3: managed standalone updates

- [x] Define the supported inspect/config surface and explicit opt-in policy.
- [x] Pull first and verify the target image before stopping the container.
- [x] Recreate with deterministic name handoff and rollback metadata.
- [x] Verify successful startup and restore the previous container on failure.
- [x] Preserve volumes, networks, restart policy, health configuration, labels,
  resource constraints, and required security settings.
- [x] Refuse Swarm tasks, Compose containers, unsafe dependency relationships,
  static addressing, auto-remove, and configurations LinuxIO cannot safely reproduce.
- [ ] Complete failure-injection coverage for pull, stop, rename, create,
  verification, and rollback; start failure and rollback are covered.

### Batch 4: scheduling and Watchtower removal

- [x] Run checks and updates from a short-lived LinuxIO systemd oneshot service.
- [x] Make scheduled selection and policy LinuxIO-owned.
- [x] Correlate run summaries with journald and persistent status records.
- [x] Migrate existing Watchtower settings without silently broadening update
  scope.
- [x] Remove the Watchtower binary, parser, service, timer, installer paths, and
  build dependency only after all call sites use the native engine.

## Safety rules

- Never update a Compose-managed container through standalone recreation.
- Authenticated private registries are out of scope until explicitly designed.
- A failed check is not equivalent to “current.”
- A pulled image is not success until the intended workload has been reconciled
  and verified.
- Cancellation stops further registry or mutation work and returns the caller's
  context error.
- Scheduled and manual operations share the same lock and operation model.

## Completion criteria

- Public registry checks work without Watchtower.
- Compose projects update through their recorded project configuration.
- Supported standalone containers update transactionally and roll back.
- Unsupported standalone configurations fail before mutation with a useful
  reason.
- Manual and scheduled updates share one mutation engine, cross-process lock,
  and persistent status shape.
- Scheduled runs are systemd-owned and remain visible through journald across
  UI connection loss.
- Unit and race tests pass. Real Docker/Compose integration, upgrade migration,
  and the remaining failure-injection matrix must pass before release.
