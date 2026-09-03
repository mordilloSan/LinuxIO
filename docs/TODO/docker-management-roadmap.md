# Docker Management Plan

> **Status:** Planned

LinuxIO will extend its current single-host Docker management without changing
the Docker module's visual language. The work covers container lifecycle,
volume browsing and backups, network management, and Docker activity.

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

## Phase 1: container lifecycle and inspect

Add typed API routes for:

- [ ] inspect container;
- [ ] pause and unpause container;
- [ ] kill container with `SIGKILL`; and
- [ ] remove container with an explicit `force` option.

Keep start, stop, restart, update, logs, terminal, and monitoring behavior.

Load inspect data when the user selects a container. Show these sections in the
current resource-details layout:

- overview and health;
- image, command, entrypoint, restart policy, user, and working directory;
- environment variables, masked by default;
- ports and labels; and
- mounts and networks.

Start or Stop remains the primary action. Restart, Pause, Unpause, Kill, Edit,
and Remove use the action menu. State guards prevent invalid actions. Kill and
Remove require clear confirmation when they can interrupt work or destroy data.

## Phase 2: create and rollback-aware edit

Use one container form for Create and Edit. It supports:

- [ ] name and image;
- [ ] command and entrypoint;
- [ ] environment variables;
- [ ] published ports;
- [ ] named volumes and bind mounts;
- [ ] networks and aliases;
- [ ] restart policy; and
- [ ] user and working directory.

Keep Basics open. Collapse optional sections unless they contain values. Create
uses a local image or pulls the image when Docker cannot find it, then starts
the container only when the user requests it.

Editing recreates a standalone container through the rollback transaction that
the native image updater already uses:

1. Inspect the original and validate the edited configuration.
2. Merge edited fields into a full copy of the original configuration.
3. Show the user a concise change summary and downtime warning.
4. Stop and rename the original as the rollback container.
5. Create, start, and verify the replacement.
6. Remove the rollback container after verification succeeds.
7. Remove a failed replacement and restore the original name and running state.

The transaction journal restores the original after a bridge or process failure.
Preflight rejects auto-remove containers, unsafe dependencies, and network
configurations that the transaction cannot preserve. Compose-managed containers
offer **Edit stack** because the Compose file owns their configuration.

This phase provides failure rollback. Container configuration history and a
manual “roll back to an older version” interface remain deferred.

## Phase 3: volumes and networks

### Volumes

- [ ] Finish Create Volume with name, driver, and optional labels.
- [ ] Remove the current forced deletion and let Docker reject in-use volumes.
- [ ] Show the containers that use each volume and their running state.
- [ ] Add **Browse in Navigator** for an accessible volume mountpoint.
- [ ] Add **Download backup** through the existing `filebrowser.archive` task.

Navigator remains the only file-management interface. Download backup creates a
ZIP archive and uses the current task progress and browser download flow. The
confirmation lists attached running containers and warns that active writers
can produce an inconsistent archive. Custom volume drivers with no accessible
host mountpoint show an explanation instead of Browse and Backup actions.

### Networks

- [ ] Send the existing Driver and Internal fields to Docker.
- [ ] Add attachable, IPv6, subnet, and gateway fields.
- [ ] Put driver options behind an Advanced section.
- [ ] Connect an unattached container, with optional aliases.
- [ ] Disconnect an attached container after confirmation.
- [ ] Protect Docker's default networks and report in-use deletion errors.

The current network details panel and connected-container table host these
actions. Docker requires recreation for network configuration changes, so this
plan does not add a misleading Edit Network action.

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
  restores; and
- network topology and host-wide ports visualizations.

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
