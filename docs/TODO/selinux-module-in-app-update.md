# SELinux module in in-app updates

## Status

Replaced, not built. Decided 2026-11-21: the fix planned here is not built, because [Package-managed installation and updates](./packaging.md) removes the installer script and the in-app updater's sandbox, and the RPM loads the module itself. This file is deleted when that lands, together with the Known limits line "An in-app update cannot load a module that is new or changed" in [Process & Systemd Architecture](../process-systemd-architecture.md#known-limits).

## The problem, until the packages ship

- An in-app update runs the target release's installer in `linuxio-updater-<n>.service`, a transient unit with `ProtectSystem=full` (`backend/bridge/handlers/appupdate/appupdate.go`). `/etc/selinux` is read-only in it.
- There `load_selinux_module` (`packaging/scripts/install-linuxio-binaries.sh`) does not try a module that is new or changed. It prints a notice and keeps the module loaded before.
- So, on the RHEL family:
  - a host updated in-app while SELinux was permissive or disabled gets no module;
  - a module change reaches a host only through an install from a shell;
  - a binary that needs a rule the old module lacks is refused sign-in until then. Sign-in is never more allowed than before.

## What to do meanwhile

A release that changes `packaging/selinux/linuxio.cil` is installed once from a root shell on each RHEL-family host, where `/etc/selinux` is writable and the installer checks the module's form before it loads it:

```bash
curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash -s -- <version>
```

The notice in the update log names `curl … -o linuxio.cil && semodule -i linuxio.cil` instead. That loads a file from `main` as root with no check of its content; prefer the installer.

## Observed on Fedora 45, 2026-11-18

These are why widening the sandbox is not the fix.

| # | Fact |
| --- | --- |
| F1 | In the updater's sandbox `semodule -i` fails with "Read-only file system" on `/etc/selinux`; the policy store and the loaded policy stay exactly as they were |
| F2 | `restorecon /usr/local/bin/linuxio-auth` works in the sandbox |
| F3 | With `/etc/selinux` in `ReadWritePaths`, every such unit fails to start with status 226, because the policy refuses `init_t` the bind mount (`mounton` on `selinux_config_t`). Any mount on that folder is ruled out |
| F4 | `semodule -i` takes about 5 s. `semodule -E linuxio` returns the stored CIL byte for byte |
| F5 | A module removed while the binary carries its type leaves the binary `unlabeled_t`, which systemd cannot run |

## Considered and not built

- `/etc/selinux` through `ReadWritePaths=` or `BindPaths=` on the updater's unit: F3.
- The updater without `ProtectSystem` for the whole installer run: the whole installer, and every packaging asset fetched from `main` with no checksum, would write anywhere under `/etc` and `/usr`.
- A second, unsandboxed step started by the job host: helps only updates started by a bridge that already has it.
- A unit that loads the module at every start of `linuxio.target`: about 600 lines, a step in front of sign-in at every boot, and its result is not in the update log.
- The installer loading the module through a transient unit it starts itself (`systemd-run --wait --pipe`, the checked file on stdin). This was the recommendation, at about 900 lines with its tests and a release mirror for the VM. It rested on one thing never checked: that a unit the sandboxed updater asks systemd to start is itself outside the sandbox.
