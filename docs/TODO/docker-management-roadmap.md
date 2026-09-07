# Docker Management Plan

> **Status:** Phases 1–3 and topology complete; Phase 4 remains open.
> Source reviewed on 2026-09-05: the Docker tab list has no Activity page, and
> the bridge Docker bindings have no audit or Engine-event stream routes.

The remaining work adds Docker activity, mutation audit records, and Engine
events to the existing single-host Docker management. It keeps the Docker
module's visual language.

“Full lifecycle” means the common container actions in this plan. It does not
mean exposing every Docker Engine option.

## UI contract

All Docker work must reuse LinuxIO's existing components and theme:

- use `App*` controls, `FrostedCard`, `AppVirtualTable`, routed tabs, and
  `DockerResourceDetailsLayout`;
- use `AppTypography` variants and `--app-*` variables;
- keep cards compact and place detailed controls in the selected-resource
  panel or a `GeneralDialog`;
- keep one primary action visible and place secondary actions in the current
  action menu;
- show status with text or icons as well as colour; and
- support keyboard use, visible focus, compact layouts, and 200% zoom.

The existing styling-boundary and shared-component tests remain release gates.
LinuxIO will not copy layouts, CSS, typography, or components from another
Docker manager.

Phases 1–3 and the topology follow-up are archived in
[Completed or closed TODOs](./completed.md#docker-management-phases-13-and-topology).

## Phase 4: activity, audit, and Docker events

Add an **Activity** Docker tab with three views.

### Operations

Reuse the task service and notification system. Show Docker tasks newest first
with queued, running, completed, failed, and cancelled states. The detail panel
shows progress, output, errors, timestamps, and a link to the affected resource.
The navbar notification menu remains the compact live view.

### Audit

Write one structured journald record for each Docker mutation, including existing
container, Compose, update, prune, image, network, and volume actions. Record:

- timestamp, username, and UID;
- action, resource type, resource name, and resource ID;
- success or failure; and
- task or operation ID when one exists.

Use the current journal pagination and follow routes to display the records.
Exclude environment values, registry credentials, and request bodies. Host
journal retention controls audit retention; LinuxIO will describe this as an
operational audit log rather than an immutable compliance log.

### Docker events

Add a typed duplex stream backed by the Moby client. Show container, image,
network, volume, and daemon events with type and action filters, text search,
and a selected-event detail panel. Reconnect from the last received timestamp
and retain at most 256 entries in the page.

Docker events do not contain a trustworthy LinuxIO actor, so the UI keeps them
separate from the audit log.

## Deferred scope

- remote Docker environments, Swarm, and Kubernetes;
- Docker resource RBAC, registries, GitOps, image builds, and vulnerability
  scanning;
- container commit, clone, rename, custom kill signals, and raw unredacted
  inspect JSON;
- scheduled, encrypted, retained, or S3 volume backups and transactional
  restores.

## Verification

Each phase needs backend tests for validation, Docker SDK mapping, cancellation,
error propagation, and mutation safety. Rollback tests must inject failures at
stop, rename, create, start, verification, and cleanup, then confirm restoration
of the original name and state.

Frontend tests must cover state-aware actions, confirmations, masked values,
form validation, query invalidation, route search state, activity filtering,
and event reconnection. Browser tests must cover the container dialogs, resource
navigation, the volume-to-Navigator handoff, and the live Activity page.

Contract changes require `make generate`. The final phase gate is:

```text
make test-quiet
make test-frontend-browser-quiet
```
