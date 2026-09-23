# Back up LinuxIO to TerraMaster TOS

LinuxIO supports two ways for **TerraMaster TOS** to pull files: a read-only
rsync module on **873**, or the same module in TOS's encryption mode through
the existing **SSH service**. The SSH port is configurable and defaults to **22**.
Scheduling, backup versions and retention belong to TOS. This is a file
backup, not a bootable disk image.

## Read-only module — port 873

1. Sign in to LinuxIO with an administrator session. The post-login capability
   scan checks whether rsync is installed.
2. If absent, open **Settings → Capabilities → rsync → Install**. Installation
   uses PackageKit, as with the other optional host dependencies.
3. Open **Shares → rsync**. Choose the existing folder to back up, a module
   name (for example `backup`), the TNAS IP address, the listener port, and a
   backup username and password. Keep the module port at **873**, or choose a
   free port. The module and SSH cannot share a listener.
4. Select **Save and start**. LinuxIO writes the configuration and credentials,
   starts the daemon, verifies its local rsync greeting, and enables startup
   after reboot. The UI shows the service state and the TOS connection fields.
5. In TOS **Centralized Backup → File Server**, add the Linux server in **rsync
   module mode**, using its LAN IP, the configured port, and the backup
   credentials, then pick the module from the backup source list. These
   credentials are independent of Linux user accounts.
6. Create the backup task, destination, schedule and retention policy in TOS.
   Verify the first backup and restore a sample file before relying on it.

The backup password is never returned to the browser. Leaving it blank when
saving keeps it; changing the backup username requires a new password.
**Stop and disable** terminates transfers and disables startup while retaining
the configuration. Saving also interrupts current transfers.

## Encryption mode — SSH, configurable port (default 22)

TOS's **rsync module encryption mode** reuses the module above through SSH.
Observed against TOS 7 on 2026-09-23, TOS logs in over SSH with a Linux
account, runs `ps -ef | grep rsync` to confirm an rsync daemon is running,
runs `cat /etc/rsyncd.conf` to learn the module names and folders, then copies
the module folder with `rsync --server --sender` inside that SSH session, as
the logged-in account. It never connects to the daemon's port in this mode.

LinuxIO therefore keeps the module configuration free of secrets, writes it
with mode `0644`, and links `/etc/rsyncd.conf` to it when that path is unused.
The SSH card shows whether encryption mode is ready: the module must be
running, and `/etc/rsyncd.conf` must be LinuxIO's link. An existing foreign
`/etc/rsyncd.conf` is left untouched and reported instead.

**Shares → rsync → SSH** also checks the existing local SSH listener on the
chosen port (default **22**; valid ports range from 1 to 65535). The check
verifies an SSH 2 identification on localhost; it does not authenticate an
account or establish connectivity from the TNAS.

1. Save and start the module. Keep it running; TOS's `ps` check fails otherwise.
2. In TOS **Centralized Backup → File Server**, add the Linux server in
   encryption mode with its LAN IP, the SSH port, and the username and SSH
   password of a Linux account that can read the module folder. Any regular
   account works, including your own. TOS stores that password, so a dedicated
   read-only account created in **Accounts** limits what a compromised NAS
   could reach. Root logins are governed by the server's SSH policy.
3. Pick the module from the backup source list and create the task.

The module password, NAS IP restriction and `read only` setting apply only to
the daemon port. In encryption mode the account's Linux permissions govern
access, and a TOS restore writes with those permissions. An ordinary account
cannot read root-only container data; use the unencrypted module for that.
This feature uses the server's existing SSH configuration; it does not change
its port or authentication policy.

## Access and service ownership

- The daemon reads as root so the selected folder can include files owned by
  root and containers. Access is restricted to the exact NAS address and
  localhost (`127.0.0.1` and `::1`). The module requires authentication.
  `list = yes` shows the module name to any client that reaches the port, so
  the TOS backup-source picker can select it; without it the picker is empty.
  Listing reveals no contents, and other addresses are still denied access.
- `read only = yes`, `use chroot = yes`, `hosts deny = *`, and
  `strict modes = yes` are fixed protections, not editable advanced options.
- LinuxIO owns `/etc/linuxio/rsyncd.conf` (mode `0644`, no secrets),
  `/etc/linuxio/rsyncd.secrets` (mode `0600`), `linuxio-rsync.service`, and
  the `/etc/rsyncd.conf` symlink when it created it. It leaves an existing
  `/etc/rsyncd.conf` and the distro rsync service untouched; a distro service
  enabled later would read the linked configuration and conflict on the port.
  Two daemons cannot
  listen on the same address and port; stop a conflicting service from
  LinuxIO's **Services** page or choose a free port.
- Configuration and service controls run in the privileged bridge. Each
  endpoint requires a privileged session. A host file lock serializes changes
  across sessions; files are written atomically. Credentials are not placed
  in command arguments or API responses.
- Logs go to the service journal, available in LinuxIO's **Services** page.
- Permit the selected TCP port from the TNAS address in the host/network
  firewall. Module mode does not encrypt file contents; keep it on a trusted
  LAN or VPN.

Choose data folders deliberately. Copying a running database or container's
files does not guarantee an application-consistent backup; use that
application's export or snapshot before the scheduled pull. Avoid selecting
virtual filesystems such as `/proc`, `/sys`, `/dev` or `/run`.

## Verification

The automated checks cover absent/present detection, login persistence,
installation UI, module setup, credentials, service controls, rejected input,
cancellation, configurable SSH ports, SSH identification (including rejecting a plain rsync listener),
the `/etc/rsyncd.conf` link (created when absent, left alone when foreign) with
the encryption-mode readiness flag, and API authorization metadata. Mocked service tests do not establish interoperability with a real TNAS or verify host firewall rules.

References: [TerraMaster Centralized Backup](https://help.terra-master.com/docs/TOS7/backup/centralized-backup/),
the [rsync daemon manual](https://download.samba.org/pub/rsync/rsyncd.conf.5),
and [rsync over remote shells](https://download.samba.org/pub/rsync/rsync.1).
