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

TOS's encrypted mode is the rsync daemon over SSH: TOS logs in with a Linux
account's SSH password, runs `rsync --daemon` as that account, and lists the
modules from `rsyncd.conf` in the account's home directory. A plain account
with no such file makes TOS report a connection failure.

**Shares → rsync → SSH** checks the existing local SSH listener on the chosen
port (default **22**; valid ports range from 1 to 65535). The check verifies an
SSH 2 identification on localhost; it does not authenticate an account or
establish connectivity from the TNAS.

1. Create a dedicated Linux account in **Accounts** and give it read access to
   the folder to back up. Do not use root: rsync ignores the home configuration
   when the SSH login is the super-user.
2. Enter that account and the source folder, then select **Save SSH module**.
   LinuxIO writes `rsyncd.conf` in the account's home, owned by the account,
   with a `backup` module: `read only = yes`, `use chroot = no` (a non-root
   daemon cannot chroot), `munge symlinks = no` so backups keep symlink targets,
   and `list = yes` so TOS can pick it. It keeps a symlink at
   `/etc/linuxio/rsyncd-ssh.conf` to find the saved module again. An existing
   hand-written `rsyncd.conf` in that home is never overwritten or deleted.
3. In TOS **Centralized Backup → File Server**, add the Linux server with its
   LAN IP, the SSH port, the account name and its SSH password, then pick the
   `backup` module from the backup source list.

**Remove** deletes the module file and the symlink. Saving with a different
account moves the module and removes the previous account's file.

The account's Linux permissions govern access; the module password and NAS IP
restriction of the port 873 module do not apply. An ordinary account cannot
read root-only container data. Choose module mode for the root-readable export
described above. This feature uses the server's existing SSH configuration; it
does not change its port or authentication policy, and the module daemon can
remain stopped.

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
  (both mode `0600`), `linuxio-rsync.service`, the `/etc/linuxio/rsyncd-ssh.conf`
  symlink and the `rsyncd.conf` it points to in the SSH account's home (mode
  `0644`, owned by that account, no secrets). It leaves an existing
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
the SSH module file (save, read back, account switch, removal, refusing root,
unknown accounts and hand-written files), and API authorization metadata. Mocked service tests do not establish interoperability with a real TNAS or verify host firewall rules.

References: [TerraMaster Centralized Backup](https://help.terra-master.com/docs/TOS7/backup/centralized-backup/),
the [rsync daemon manual](https://download.samba.org/pub/rsync/rsyncd.conf.5),
and [rsync over remote shells](https://download.samba.org/pub/rsync/rsync.1).
