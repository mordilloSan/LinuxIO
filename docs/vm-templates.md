# VM templates

VMs → Templates manages the ready images used by Home Assistant OS, Debian,
Ubuntu and Fedora. Templates are saved in
`/var/lib/libvirt/images/linuxio/templates/<preset>/<download-sha256>/`.
The manager shows each version, download date, image size, source URL, checksum
and full image path. ISO installers remain in the existing ISO folder.

Creating the first VM downloads and saves its template automatically. Later
creates reuse the newest saved version without contacting the publisher;
the create dialog can also select a specific saved version. A missing selected
version fails instead of silently choosing or downloading a different one.
Each VM receives an independent qcow2 disk under `linuxio/cloud-images`.
Templates contain no VM login credentials or cloud-init seed data.
Deleting a VM with **Delete LinuxIO-managed disks** selected removes its owned
disk and cloud-init seed even when libvirt does not list them as storage volumes.
Saved templates and external disks are preserved.
Disks kept by a delete without that option appear on the **Disks** tab
(`virt.unused_disks`): LinuxIO-named disks and seeds in the default pool and
`linuxio/cloud-images` that no defined VM references by file name and whose VM
name no longer exists. `virt.unused_disk_delete` re-checks that under a per-VM
name file lock that `virt.create` also holds, so a disk a create is still
writing is never removed.

**Download update** explicitly checks the publisher and saves a new version
when needed. Home Assistant OS records the release tag and uses versioned
release URLs; Fedora records the configured release/build. Debian and Ubuntu
use rolling publisher URLs: their OS version, download date and SHA-256 identify
each saved snapshot. Conditional requests reuse an unchanged download when the
publisher provides ETag or Last-Modified validators. Without validators, an
explicit update may transfer the image again; identical content is stored once.
Old versions remain available until deleted. Downloading updates does not update
the operating systems inside existing VMs.

Deletion removes only the chosen saved version. Existing VMs continue to use
their independent disks. Deleting all saved versions means the next create must
download again. Images used by VMs created before this feature cannot safely be
recovered as pristine templates; their first new create downloads a base once.

All operations remain in the privileged bridge. `virt.templates` lists the
library; `virt.template_delete` accepts a preset and a validated template ID;
`virt.template_download` is a session-bound Task with creation-style progress.
`virt.create` accepts an optional `templateId`. Generated query descriptors and
the operation invalidation manifest refresh the manager after mutations and
recovered Task completion.

Per-preset process locks protect downloads, copies and deletion across sessions.
Only complete, validated images and metadata are published by atomic directory
rename. Failed or canceled downloads remain unpublished; the next attempt also
cleans staging files left by a terminated bridge. Downloads must be standalone
qcow2 images; backing files and external data files are rejected before QEMU
opens them, using the [QEMU header format](https://www.qemu.org/docs/master/interop/qcow2.html).

Automated unit tests use local HTTP fixtures and real filesystem locks, with
QEMU/XZ command doubles. Real libvirt VM creation remains a host integration
check.
