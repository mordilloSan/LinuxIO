# Mutation map

Date: 2026-10-25. Input to the jobs design: work that must outlive the login
session and the per-login bridge.

These are the API routes that change something on the host: 205 of the 332. Each route was
traced from its registration to the code that does the work. The map comes from reading
the code: nothing was run, and durations come from the code's own caps and
waits, not measurements. To find a route's code, run
`rg -n '"<route>"' backend/bridge/handlers`.

## How work ends today

| Kind | Runs in | What stops it |
|---|---|---|
| Call (173) | the bridge, on the request's context | The browser's 30 s timeout aborts the stream and cancels the context. Child processes get SIGKILL; a request already handed to systemd, Docker, libvirt or PackageKit still finishes there, and only the reply is lost. Also stopped by everything that stops a session task. |
| Session task (31) | the bridge | Logout. A LinuxIO update: it restarts `linuxio.target`, which stops every login's bridge. The 12 h session limit. A bridge crash. 15 min without activity: progress frames reaching an open tab count as activity, so the clock starts when the tab closes. `device_remove` reports no progress after it starts, so it times out even with the tab open. On shutdown the bridge waits at most 5 s, so cleanup after the cancel (removing a partial copy, cancelling a balance) often does not run. |
| Durable task (1) | a systemd transient unit running a root worker | Only its own cancel. A later login by the same user reattaches. This is `docker.update_container`. |

The kernel side was checked against the btrfs source in kernel master:

- Balance and device remove stop when the calling thread gets a fatal signal: `btrfs_should_cancel_balance` checks `fatal_signal_pending`.
- Scrub stops on any pending signal: `should_cancel_scrub` returns `-EINTR`. Device replace runs on the scrub code.

Killing the bridge therefore stops all four. The pool stays consistent, but the work has to start again.

## Classes

Each route gets one class: the first in this list that fits.

| Class | Count | Meaning | What it needs |
|---|---:|---|---|
| B browser-bound | 3 | Bytes flow to or from the browser | Stays tied to the connection and resumes by offset |
| D durable | 4 | Already survives the bridge | Nothing |
| J long job | 34 | Minutes to days of work that LinuxIO drives itself; it stops when the bridge dies | An owner outside the bridge |
| X external long | 5 | The long part already runs in the kernel, a drive, a daemon or systemd; the bridge only waits | A way to find and follow it again after login |
| S self-severing | 9 | Cuts the caller's own connection: reboot, power off, a network change on the NIC in use | Hand-off before the connection drops; never retried |
| C long call | 14 | A Call that can outlast the 30 s browser timeout | Must not be cancelled by that timeout, or becomes a job |
| M multi-step | 41 | Quick but several steps; a bridge death between steps leaves it half-done | Ordered or atomic steps that a retry repairs |
| Q quick | 89 | One request to systemd, Docker, libvirt or the kernel, or one atomic write; a dead bridge loses only the reply | Nothing |
| R not a change | 6 | Cache refreshes, a read-only check, task control | Nothing |

Column values used below:

- Time: `<1s`, `seconds`, `>30s` (can outlast the browser timeout), `long` (minutes to days).
- Bridge dies: `reply lost` (the daemon or kernel finishes), `stops` (the work stops part-way), `half-done` (between steps), `harmless`.

## Storage: btrfs pools and devices

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| storage.btrfs_pool_create | task | partitions disks, runs mkfs, mounts, adds fstab line | mkfs.btrfs, ioctls, mount | long (mkfs trims every disk) | stops: disks left partitioned or half-formatted, no rollback | J |
| storage.btrfs_pool_destroy | call | unmounts, drops fstab lines, wipes member superblocks | umount2, fstab, superblock wipe | >30s (unmount flushes dirty data) | half-done; a retry finishes | C |
| storage.btrfs_pool_mount | call | mounts an offline pool, adds fstab line | superblock scan, mount(2), fstab | >30s (log replay, spun-down disks) | half-done: mounted with no fstab or pool-state record | C |
| storage.btrfs_device_add | call | adds a free disk to a pool | lsblk, ioctl ADD_DEV | seconds | harmless | Q |
| storage.btrfs_device_grow | call | grows a device to its disk size | ioctl RESIZE | <1s | harmless | Q |
| storage.btrfs_device_remove | task | moves data off a disk, drops it | ioctl RM_DEV_V2, blocking for the whole run | long | stops; the disk stays in the pool | J |
| storage.btrfs_device_replace | task | copies a member onto a new disk | ioctl DEV_REPLACE, polled | long | stops | J |
| storage.btrfs_balance | task | rewrites chunks, converts profiles | ioctl BALANCE_V2, polled | long | stops; profiles left part-converted, consistent | J |
| storage.btrfs_balance_cancel | call | stops a running balance | ioctl BALANCE_CTL | seconds | harmless | Q |
| storage.btrfs_scrub_start | task | verifies and repairs every copy | ioctl SCRUB per device, polled | long (hours) | stops | J |
| storage.btrfs_scrub_cancel | call | stops a running scrub | ioctl SCRUB_CANCEL | seconds | harmless | Q |
| storage.btrfs_set_label | call | renames the filesystem | ioctl SET_FSLABEL | <1s | harmless | Q |
| storage.btrfs_set_compression | call | remounts with compress=, rewrites fstab | remount, fstab | seconds | half-done: remounted, fstab not updated | M |
| storage.btrfs_set_quotas | call | turns quotas on or off | ioctl QUOTA_CTL | seconds | harmless (the rescan continues in the kernel) | Q |
| storage.btrfs_qgroup_limit | call | sets a subvolume size limit | ioctl QGROUP_LIMIT | <1s | harmless | Q |

## Storage: subvolumes, snapshots, replication

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| storage.btrfs_subvolume_create | call | creates a subvolume | ioctl SUBVOL_CREATE, chmod | <1s | harmless | Q |
| storage.btrfs_subvolume_delete | call | deletes a subvolume | ioctl SNAP_DESTROY | <1s | harmless (the kernel reclaims space later) | Q |
| storage.btrfs_subvolume_set_compression | call | sets the compression property | setxattr | <1s | harmless | Q |
| storage.btrfs_subvolume_set_default | call | changes the default subvolume | ioctl DEFAULT_SUBVOL | <1s | harmless | Q |
| storage.btrfs_subvolume_set_readonly | call | toggles read-only | ioctl SUBVOL_SETFLAGS | <1s | harmless | Q |
| storage.btrfs_snapshot_create | call | takes a manual snapshot | ioctl SNAP_CREATE_V2 | <1s | harmless | Q |
| storage.btrfs_snapshot_delete | call | deletes a snapshot | ioctl SNAP_DESTROY | <1s | harmless | Q |
| storage.btrfs_snapshot_rollback | task | swaps a subvolume for a copy of a snapshot | 2 snapshots, 2 renames, set-default | seconds | half-done: between the renames the subvolume path is missing | J |
| storage.btrfs_snapshot_rule_set | call | saves a snapshot rule, syncs the timer | rules file, unit files, systemd | seconds | half-done: rule saved, timer stale | M |
| storage.btrfs_snapshot_rule_delete | call | removes a rule, syncs the timer | same | seconds | half-done | M |
| storage.btrfs_snapshot_run_now | call | takes due snapshots, prunes old ones | lock (waits up to 5 min), ioctls | >30s (waits for the hourly run's lock) | stops; the next run catches up | C |
| storage.btrfs_replication_job_set | call | saves a replication job, syncs the timer | jobs file, unit files, systemd | seconds | half-done | M |
| storage.btrfs_replication_job_delete | call | removes a job, syncs the timer | same | seconds | half-done | M |
| storage.btrfs_replication_run | task | sends the newest snapshot to the target | ioctl SEND into btrfs receive or ssh | long (hours) | stops: partial copy on the target, outcome not recorded | J |
| storage.btrfs_replication_ssh_key | call | creates the replication key, once | ssh-keygen | <1s | harmless | Q |
| storage.btrfs_replication_ssh_trust | call | stores approved host keys | ssh-keyscan, known_hosts | seconds | harmless | Q |
| storage.set_btrfs_maintenance | call | edits the btrfsmaintenance config | in-place file write, systemd start | seconds | half-done: a kill mid-write leaves a truncated file | M |

`snapshot_run_now` and `replication_run` run the same code as the hourly
`linuxio-btrfs-snapshot.service`, which already runs outside the bridge with
cross-process locks.

## Storage: md RAID, LVM, SMART, network mounts

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| storage.md_member_add | call | adds a disk to an array | mdadm --add | seconds | harmless (the rebuild runs in the kernel) | Q |
| storage.md_member_fail | call | marks a member faulty | sysfs | <1s | harmless | Q |
| storage.md_member_remove | call | detaches a faulty or spare member | sysfs | <1s | harmless | Q |
| storage.md_member_replace | call | adds a spare, flags a member for replacement | mdadm --add, sysfs | seconds | half-done: spare added, not flagged | M |
| storage.md_sync_action | call | starts or stops check/repair | sysfs | <1s | reply lost (the kernel runs it for hours; nothing follows it) | X |
| storage.create_lv | call | creates a logical volume | lvcreate | seconds | stops | Q |
| storage.delete_lv | call | removes a logical volume | lvremove | seconds | stops | Q |
| storage.resize_lv | call | resizes a volume and its filesystem | lvresize -r (fsadm, resize2fs, xfs_growfs) | long | stops: volume and filesystem sizes can disagree | J |
| storage.run_smart_test | task | runs a drive self-test | smartctl -t, then polling | long (2 min to 30 h) | reply lost (the test runs in the drive) | X |
| storage.mount_nfs | call | mounts an NFS share, records it | mount -t nfs, fstab or registry | seconds (own 30 s cap) | half-done: mounted, not recorded | M |
| storage.remount_nfs | call | unmounts and remounts with new options | umount, mount, fstab | >30s (15 + 30 s caps) | half-done: share left unmounted | C |
| storage.unmount_nfs | call | unmounts NFS, optionally drops the fstab line | umount, fstab | seconds | harmless | Q |
| storage.mount_cifs | call | stores credentials and fstab line, mounts | credentials file, fstab, mount.cifs | seconds (own 30 s cap) | half-done: fstab line and credentials stay, not mounted | M |
| storage.remount_cifs | call | unmounts and remounts with new options | umount, fstab, mount, rollback mount | >30s (15 + 30 + 30 s caps) | half-done: share left unmounted | C |
| storage.unmount_cifs | call | unmounts SMB, optionally drops fstab line and credentials | umount, fstab | seconds | harmless | Q |
| storage.unmount_filesystem | call | unmounts any non-system filesystem | umount2 | >30s (flushes dirty data) | harmless | C |

## Storage: partitions

Added by the partition feature on 2026-10-25, after the rest of the map was traced.

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| storage.disk_initialize | call | replaces the partition table with an empty GPT | GPT write, partition-table reread | <1s | harmless | Q |
| storage.partition_create | task | creates a partition; optionally formats and mounts it | GPT write, udev wait, mkfs, mount(2), fstab | long when it formats (mkfs trims) | half-done: created but not formatted, or formatted but not mounted; the error says which | J |
| storage.partition_format | task | makes a filesystem on a partition | mkfs.ext4, xfs or btrfs, with trim | long | stops: partition half-formatted; rerun with overwrite | J |
| storage.partition_mount | call | mounts a partition, optionally adds an fstab line | mount(2), fstab | seconds | half-done: mounted, fstab not written | M |
| storage.partition_delete | call | removes an unused partition from the GPT | GPT write | <1s | harmless | Q |

## Docker

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| docker.start_container | call | starts a container | Docker API | seconds | reply lost | Q |
| docker.stop_container | call | stops a container | Docker API | seconds (>30s with a long stop grace) | reply lost | Q |
| docker.restart_container | call | restarts a container | Docker API | seconds (>30s with a long stop grace) | reply lost | Q |
| docker.kill_container | call | kills a container | Docker API | <1s | reply lost | Q |
| docker.pause_container | call | freezes a container | Docker API | <1s | reply lost | Q |
| docker.unpause_container | call | thaws a container | Docker API | <1s | reply lost | Q |
| docker.remove_container | call | deletes a container | Docker API | seconds | reply lost | Q |
| docker.start_all_stopped | call | starts every stopped container | Docker API, one at a time | >30s | stops part-way | C |
| docker.stop_all_running | call | stops every running container | Docker API, one at a time, 10 s grace each | >30s | stops part-way | C |
| docker.create_container | call | pulls an image, creates and optionally starts a container | Docker API, readiness wait up to 90 s | long (image pull) | stops: image pulled, container created but not verified | J |
| docker.edit_container | call | recreates a container with new settings | journaled stop, rename, create, start, wait | long (pull, readiness wait up to 90 s) | half-done: the original stays stopped and renamed until the next recovery pass | J |
| docker.update_container | durable task | pulls the new image, recreates the container | systemd transient unit with a root worker | long | survives; reattached at the next login | D |
| docker.set_container_auto_update | call | writes the auto-update config and timer | 3 files, systemd | seconds | half-done | M |
| docker.check_updates | call | refreshes the update-status cache | registry lookups through Docker | minutes (retry backoff 5 + 10 + 20 + 40 + 80 s) | harmless | R |
| docker.check_container_update | call | refreshes one cache entry | same | minutes (backoff) | harmless | R |
| docker.compose | task | runs up, down, stop or restart on a stack, with progress | docker compose CLI | long (pull, build, health waits) | stops: stack half-reconciled | J |
| docker.compose_up | call | starts a stack, pulling missing images | docker compose CLI | long | stops: stack half-up | J |
| docker.compose_down | call | removes a stack's containers and networks | docker compose CLI | >30s (stop grace per container) | stops: stack half-down | J |
| docker.compose_stop | call | stops a stack | docker compose CLI | >30s | stops part-way | J |
| docker.compose_restart | call | re-applies a stack (`up -d --remove-orphans`) | docker compose CLI | long | stops part-way | J |
| docker.delete_stack | call | takes a stack down, deletes its files | compose down, recursive delete | >30s | half-done: directory half-deleted | J |
| docker.system_prune | call | deletes unused containers, images, build cache, volumes | Docker API, 5 prune requests | long (gigabytes) | stops; later phases never run | J |
| docker.delete_image | call | removes an image | Docker API | seconds | reply lost | Q |
| docker.create_network | call | creates a network | Docker API | <1s | reply lost | Q |
| docker.delete_network | call | removes a network | Docker API | <1s | reply lost | Q |
| docker.connect_network | call | attaches a container to a network | Docker API | <1s | reply lost | Q |
| docker.disconnect_network | call | detaches a container | Docker API | <1s | reply lost | Q |
| docker.create_volume | call | creates a volume | Docker API | <1s | reply lost | Q |
| docker.delete_volume | call | removes a volume and its data | Docker API | seconds | reply lost | Q |
| docker.set_daemon_config | call | writes daemon.json, reloads dockerd | file, systemd reload | <1s | harmless | Q |
| docker.apply_daemon_storage | task | moves Docker's data root; stops Docker | stop all, systemd, drop-in, daemon.json, start | long | half-done: Docker and every container can stay stopped | J |
| docker.clear_icon_cache | call | clears cached icons | file delete | <1s | harmless | Q |
| docker.get_icon | call | fetches and caches an icon | HTTP, cache file | seconds | harmless | R |
| docker.get_icon_uri | call | same, returned as a data URI | HTTP, cache file | seconds | harmless | R |

## Virtual machines

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| virt.create | task | creates a VM: disk, seed image, definition | libvirt, qemu-img, download | long | stops: staging dir left, a disk can exist without a VM | J |
| virt.template_download | task | downloads a cloud image template | HTTP, xz, qemu-img | long | stops; no half template (published by rename) | J |
| virt.delete | call | undefines a VM, deletes its disks | libvirt, file deletes | >30s (force off, large disks) | half-done: VM gone, disks left | C |
| virt.template_delete | call | deletes a saved template | lock, recursive delete | >30s (waits while a download holds the lock) | harmless (junk left) | C |
| virt.update | call | redefines a VM; may rename files, grow a disk | renames, qemu-img resize, libvirt | seconds | half-done: the undo runs only inside the bridge | M |
| virt.start | call | starts a VM, repairing disk permissions first | chown, libvirt define and start | seconds | reply lost (qemu belongs to libvirtd) | Q |
| virt.shutdown | call | asks the guest to shut down | libvirt | <1s | reply lost | Q |
| virt.reboot | call | asks the guest to reboot | libvirt | <1s | reply lost | Q |
| virt.force_off | call | kills the VM | libvirt | seconds | reply lost | Q |
| virt.suspend | call | pauses the VM | libvirt | <1s | reply lost | Q |
| virt.resume | call | resumes the VM | libvirt | <1s | reply lost | Q |
| virt.set_autostart | call | sets autostart | libvirt | <1s | reply lost | Q |
| virt.unused_disk_delete | call | deletes an orphaned disk | lock, libvirt, unlink | seconds | harmless | Q |

## Files

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| filebrowser.copy_batch | task | copies files and folders | bridge file I/O | long | stops: partial files at the destination | J |
| filebrowser.move_batch | task | moves files and folders | rename, or copy then delete across filesystems | long across filesystems | stops: items split between places | J |
| filebrowser.resource_patch | task | renames or moves one path | rename, or copy then delete across filesystems | long across filesystems | stops | J |
| filebrowser.delete_batch | task | deletes permanently | bridge recursive delete | >30s (big trees) | stops: tree half-deleted | J |
| filebrowser.trash_purge | task | deletes trashed items permanently | recursive delete | >30s | stops: item half-deleted | J |
| filebrowser.compress | task | creates an archive | bridge zip or tar.gz into a hidden .part file | long | stops: hidden .part left | J |
| filebrowser.extract | task | extracts an archive into a new folder | bridge | long | stops: partial tree left | J |
| filebrowser.chmod_batch | task | changes mode or owner recursively | bridge | >30s (big trees) | stops: mixed modes; safe to re-run | J |
| filebrowser.trash_batch | task | moves items to the trash | rename per item | seconds | harmless | Q |
| filebrowser.trash_restore | task | restores trashed items | rename per item | seconds | harmless | Q |
| filebrowser.resource_post | call | creates an empty file or folder | mkdir or create | <1s | harmless | Q |
| filebrowser.upload | task | writes one uploaded file (also editor save) | browser bytes into a .part, then rename | client-bound | needs the browser | B |
| filebrowser.upload_batch | task | writes many uploaded files | same | client-bound | needs the browser | B |
| filebrowser.archive | task | builds an archive and streams it to the browser | temp archive, download stream | client-bound | needs the browser | B |
| filebrowser.index | task | rebuilds the search index | indexer daemon | long | reply lost (the indexer keeps scanning) | X |

## Packages, system, LinuxIO itself

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| packages.update | task | installs selected updates | PackageKit, one transaction per package | long | stops after the package in flight (PackageKit finishes that one) | J |
| updates.apply_offline_updates | call | downloads updates, arms install at reboot | snapshot, PackageKit download, offline trigger | long (about 6.5 min of waits) | stops: the download may finish, the trigger never runs | J |
| updates.refresh_cache | call | refreshes package metadata | PackageKit | >30s (waits up to 2 min) | reply lost (PackageKit finishes) | X |
| updates.set_auto_updates | call | writes automatic-update config and timers | files, systemd | seconds | half-done | M |
| system.install_capability | task | installs packages, enables a service | scripts, PackageKit, systemd | long | stops: script steps killed (dpkg may need repair); the PackageKit step finishes but enable and start never run | J |
| system.dismiss_failed_login_alert | call | stores a dismissal | user config file | <1s | harmless | Q |
| system.dismiss_unclean_shutdown | call | stores a dismissal | user config file | <1s | harmless | Q |
| control.app_update | task | installs a new LinuxIO, restarts it | installer in a transient unit, then restart of `linuxio.target` | long | half-done: the installer finishes, but the status write and the final restart run in the bridge | J |
| control.reboot | call | reboots the host | logind | <1s | reply lost | S |
| control.power_off | call | powers off the host | logind | <1s | reply lost | S |
| control.logoff | call | ends a logind session | logind | <1s | reply lost | S |

## Indexer, monitoring, schedules, power

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| indexer.set_config | call | sets indexer exclusions | indexer daemon | <1s | harmless | Q |
| indexer.set_timer_interval | call | sets the periodic index timer | drop-in, systemd | seconds | half-done | M |
| monitoring.set_config | call | changes monitoring settings | monitoring daemon | <1s | harmless | Q |
| monitoring.restart | call | restarts the monitoring daemon | systemd, then a readiness wait up to 60 s | >30s | reply lost | C |
| monitoring.refresh_smart | call | rescans drives, rewrites the SMART cache | daemon runs smartctl per drive | >30s (serial; sleeping drives) | stops (the daemon cancels when the caller leaves) | C |
| monitoring.maintain_database | call | prunes old history | daemon, one transaction | seconds | harmless | Q |
| monitoring.check_database | call | runs an integrity check, changes nothing | daemon | seconds | harmless | R |
| schedules.create | call | creates a scheduled script, disabled | files, systemd reload | seconds | half-done, recoverable | M |
| schedules.update | call | rewrites a schedule | files, systemd | seconds | half-done: timer left stopped | M |
| schedules.delete | call | removes a schedule | systemd, files | seconds | half-done; a repeat finishes | M |
| schedules.enable | call | enables and starts its timer | files, systemd | seconds | half-done | M |
| schedules.disable | call | stops and disables its timer | systemd, file | seconds | half-done | M |
| schedules.run_now | call | runs the script now | systemd starts its oneshot service | <1s | reply lost (the script runs under systemd) | X |
| schedules.stop | call | stops a running script | systemd | <1s | reply lost | Q |
| power.start | call | starts TuneD | systemd, D-Bus | seconds | reply lost | Q |
| power.set_profile | call | switches the TuneD profile | D-Bus | seconds | reply lost | Q |
| power.disable | call | disables TuneD tunings | D-Bus | <1s | reply lost | Q |

## Accounts

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| accounts.create_user | call | creates an account, its home and password | useradd, chpasswd | <1s | half-done: account with no password; a retry says it exists | M |
| accounts.modify_user | call | changes name, shell, groups or home | usermod | <1s; >30s when moving the home | stops mid-move | C |
| accounts.delete_user | call | deletes the account and its home | userdel -r | >30s (big home) | stops: home partly deleted | C |
| accounts.change_password | call | sets a password | chpasswd | <1s | harmless | Q |
| accounts.lock_user | call | locks the password | passwd -l | <1s | harmless | Q |
| accounts.unlock_user | call | unlocks the password | passwd -u | <1s | harmless | Q |
| accounts.create_group | call | creates a group | groupadd | <1s | harmless | Q |
| accounts.delete_group | call | deletes a group | groupdel | <1s | harmless | Q |
| accounts.modify_group_members | call | replaces a group's members | gpasswd -M | <1s | harmless | Q |
| accounts.terminate_session | call | ends a login session | logind | <1s | reply lost | Q |

## Alerts, settings, time, hostname

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| alerts.dismiss | call | dismisses an alert | alert store | <1s | harmless | Q |
| alerts.mark_seen | call | marks alerts seen | alert store | <1s | harmless | Q |
| alerts.mark_all_seen | call | marks all alerts seen | alert store | <1s | harmless | Q |
| alerts.set_notifications | call | email and webhook settings | file | <1s | harmless | Q |
| alerts.test_notifications | call | sends a test email or webhook | SMTP, HTTP (15 s cap) | seconds | harmless | Q |
| config.set | call | user settings, plus a Docker mount-order drop-in | file, drop-in, systemd reload | <1s | half-done: settings saved, drop-in not | M |
| config.set_ui | call | UI preferences | file | <1s | harmless | Q |
| logging.set_debug | call | debug logging for every daemon | 3 drop-ins, systemd reloads | seconds | half-done: some drop-ins written | M |
| datetime.set_timezone | call | timezone | timedated | <1s | reply lost | Q |
| datetime.set_server_time | call | clock | timedated | <1s | reply lost | Q |
| datetime.set_ntp | call | NTP on or off | timedated | seconds | reply lost | Q |
| datetime.set_ntp_servers | call | NTP server list | file, service restart | <1s | half-done: written, not restarted | M |
| hostname.set_hostname | call | hostname | hostnamed | <1s | reply lost | Q |

## Network

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| network.set_ipv4 | call | IPv4 to DHCP | config write, then NetworkManager, netplan, networkd or ifupdown apply | seconds (>30s with ifup or nmcli waits) | half-done: written, not applied; ifupdown can leave the NIC down | S |
| network.set_ipv4_manual | call | static IPv4 | same | same | same | S |
| network.set_ipv6 | call | IPv6 mode | same | same | same | S |
| network.set_mtu | call | MTU | same | same | same | S |
| network.enable_connection | call | brings a link up | nmcli, ifup or netplan | >30s (nmcli waits up to 90 s) | stops | S |
| network.disable_connection | call | takes a link down | nmcli, ifdown or netlink | seconds | half-done | S |
| network.create_bridge | call | creates a host bridge on a spare NIC | NetworkManager checkpoint, netplan try or networkd files | seconds | NetworkManager and netplan roll back by themselves; networkd has no rollback | M |
| network.set_optional | call | netplan `optional` flag | YAML, netplan generate | seconds | half-done | M |
| network.start_bridge_handoff | call | moves the host IP onto a bridge | durable record, NetworkManager checkpoint or netplan try | seconds (up to 60 s, detached) | survives; reverts unless confirmed | D |
| network.confirm_bridge_handoff | call | commits the handoff | durable record, NetworkManager or netplan | seconds | survives; the native timer reverts otherwise | D |
| network.revert_bridge_handoff | call | rolls the handoff back | NetworkManager or netplan | seconds | survives | D |

The network routes are self-severing only when they target the NIC that
carries the session. The bridge itself is local, so it finishes the change;
the browser loses the reply.

## Shares

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| shares.create_nfs_share | call | adds an export, creates its folder | /etc/exports, exportfs -ra | seconds (>30s on slow client DNS) | half-done: file written, exports not reloaded | M |
| shares.update_nfs_share | call | changes an export | same | same | same | M |
| shares.delete_nfs_share | call | removes an export | same | same | same | M |
| shares.create_samba_share | call | adds a share to smb.conf | smb.conf, smbcontrol reload | <1s | half-done: written, not reloaded | M |
| shares.update_samba_share | call | changes a share | same | <1s | same | M |
| shares.delete_samba_share | call | removes a share | same | <1s | same | M |
| shares.save_rsync | call | rsync module, secrets file and unit | stop service, 4 files, enable, start | seconds | half-done: service can stay stopped | M |
| shares.stop_rsync | call | stops and disables rsync | systemd | seconds | half-done: stopped, still enabled | M |

## systemd services

All nine return once systemd has queued the job, not when it finishes.

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| systemd.start_service | call | queues a start | systemd | <1s | reply lost | Q |
| systemd.stop_service | call | queues a stop | systemd | <1s | reply lost | Q |
| systemd.restart_service | call | queues a restart | systemd | <1s | reply lost | Q |
| systemd.reload_service | call | queues a reload | systemd | <1s | reply lost | Q |
| systemd.enable_service | call | enables at boot | systemd | <1s | harmless | Q |
| systemd.disable_service | call | disables at boot | systemd | <1s | harmless | Q |
| systemd.mask_service | call | masks | systemd | <1s | harmless | Q |
| systemd.unmask_service | call | unmasks | systemd | <1s | harmless | Q |
| systemd.reset_failed_service | call | clears the failed state | systemd | <1s | harmless | Q |

## WireGuard

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| wireguard.add_interface | call | writes a tunnel config, brings it up | in-place config write, wg-quick up | seconds | half-done: config left, tunnel down; a retry says it exists | M |
| wireguard.update_interface | call | port, MTU, hooks, DNS | wg-quick down, write, wg-quick up | seconds | half-done: tunnel left down | M |
| wireguard.remove_interface | call | removes a tunnel and its keys | wg-quick down, deletes | seconds | half-done | M |
| wireguard.up_interface | call | brings a tunnel up | wg-quick up | seconds | half-done: link half-configured | M |
| wireguard.down_interface | call | takes a tunnel down | wg-quick down | seconds | half-done: NAT rules left behind | M |
| wireguard.enable_interface | call | starts it at boot | systemd | <1s | harmless | Q |
| wireguard.disable_interface | call | stops starting it at boot | systemd | <1s | harmless | Q |
| wireguard.add_peer | call | adds a peer | in-place files, wgctrl | seconds | half-done | M |
| wireguard.update_peer | call | changes a peer | files, wgctrl | <1s | half-done | M |
| wireguard.remove_peer | call | removes a peer | config rewrite, wgctrl | <1s | half-done: the revoked peer stays live in the kernel | M |

## Task control

| Route | Kind | Changes | Done by | Time | Bridge dies | Class |
|---|---|---|---|---|---|---|
| tasks.cancel | call | cancels one of the caller's tasks | bridge memory | <1s | harmless | R |

## Found on the way

None of these are fixed. Items 1 to 3, 8 and 10 were confirmed in the code;
item 7 is an inference that needs a test.

1. Cancelling `storage.btrfs_device_remove` does not stop the kernel. `RemoveDevice` uses its context only for the lookup and never issues a cancel. The task reads "canceled" while the disk keeps emptying, which contradicts `docs/btrfs.md`.
2. If the bridge dies during `docker.apply_daemon_storage` after Docker has stopped, Docker and every container stay down.
3. `storage.btrfs_pool_create` has no rollback. A failure or kill leaves disks partitioned or half-formatted.
4. These 13 routes need root but are not declared `Privileged()`:
   - `storage.create_lv`, `storage.delete_lv`, `storage.resize_lv`
   - the three NFS and three CIFS mount routes
   - `storage.unmount_filesystem`, `storage.set_btrfs_maintenance`, `storage.run_smart_test`
   - `docker.set_container_auto_update`

   A non-root login sees them and gets a permission error. The bridge runs as that user, so this is not a privilege escalation.
5. `/etc/default/btrfsmaintenance` and the WireGuard configs are rewritten in place, not atomically.
6. Nothing stops a caller from cutting their own connection. `systemd.stop_service` and `systemd.restart_service` accept LinuxIO's own units, and the network routes accept the NIC that carries the session.
7. Scrub stops on any signal delivered to its ioctl thread, and `fsIoctl` does not retry `EINTR`. A Go process can receive process-wide signals on any thread, so a scrub could end `aborted` with nobody cancelling it. This has not been observed.
8. `useTaskAction` has no callers left. It is the only path that still makes the third round trip (`tasks.get`).
9. `docs/api-contract.md` says 24 task routes are session-bound; there are 29.
10. Some reads change things:
    - `storage.btrfs_list` enables the btrfs health timer and the degraded-boot service, and can send alert email or webhooks.
    - The subvolume, snapshot and settings reads mount a pool's top level under `/run/linuxio/btrfs/` when it is not already mounted. From then on `pool_destroy` refuses the pool: `poolMountpoints` rejects protected mounts, as `TestPoolMountpointsDeepestFirst` pins.
