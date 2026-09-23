# Back up LinuxIO to TerraMaster TOS

LinuxIO supports two ways for **TerraMaster TOS** to pull files: a read-only
rsync module on **873**, or rsync over the existing **SSH service**. The SSH
port is configurable and defaults to **22**.
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

## SSH — configurable port (default 22)

**Shares → rsync → SSH** displays the second connection mode and initially
checks the existing local SSH listener on the standard port **22**. Set
**SSH port** to the port your server uses (for example **9222**) and select
**Check SSH port** to verify it. Valid ports range from 1 to 65535. The check
verifies an SSH 2 identification on localhost; it does not authenticate an
account or establish connectivity from the TNAS.

Choose a Linux username and source folder in the UI, then use those values
in TOS's **rsync over SSH** mode, with the Linux server's LAN IP and the
selected SSH port. Enter the Linux account's SSH credentials in TOS. LinuxIO does not
collect or store them. Linux accounts can be managed in **Accounts**, and the
existing SSH service in **Services**. This feature uses the server's existing
SSH configuration; it does not change its port or authentication policy.

SSH encrypts the transfer and starts rsync under the authenticated Linux
account. The module daemon can remain stopped. The module password, NAS IP
restriction and enforced read-only behavior do **not** apply to SSH: the
account's Linux permissions govern access. An ordinary account cannot read
root-only container data. Choose module mode for the root-readable,
read-only export described above.

## Access and service ownership

- The daemon reads as root so the selected folder can include files owned by
  root and containers. Access is restricted to the exact NAS address and
  localhost (`127.0.0.1` and `::1`). The module requires authentication.
  `list = yes` shows the module name to any client that reaches the port, so
  the TOS backup-source picker can select it; without it the picker is empty.
  Listing reveals no contents, and other addresses are still denied access.
- `read only = yes`, `use chroot = yes`, `hosts deny = *`, and
  `strict modes = yes` are fixed protections, not editable advanced options.
- LinuxIO owns `/etc/linuxio/rsyncd.conf`, `/etc/linuxio/rsyncd.secrets`
  (both mode `0600`) and `linuxio-rsync.service`. It leaves an existing
  `/etc/rsyncd.conf` and distro rsync service untouched. Two daemons cannot
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
and API authorization metadata. Mocked service tests do not establish interoperability with a real TNAS or verify host firewall rules.

References: [TerraMaster Centralized Backup](https://help.terra-master.com/docs/TOS7/backup/centralized-backup/),
the [rsync daemon manual](https://download.samba.org/pub/rsync/rsyncd.conf.5),
and [rsync over remote shells](https://download.samba.org/pub/rsync/rsync.1).
