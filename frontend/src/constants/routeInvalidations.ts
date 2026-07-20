import type { QueryKey } from "@tanstack/react-query";

// Query-route key prefix: ["linuxio", handler, command] matches every cached
// instance of that route (any request variant) via TanStack partial matching.
const key = (route: string): QueryKey => ["linuxio", ...route.split(".")];

const CONTAINER_STATE_KEYS = [
  key("docker.list_containers"),
  key("docker.list_compose_projects"),
];
const CONTAINER_IMAGE_KEYS = [
  ...CONTAINER_STATE_KEYS,
  key("docker.list_images"),
];
const COMPOSE_KEYS = [
  key("docker.list_compose_projects"),
  key("docker.list_containers"),
];

// Filebrowser listing caches. Fresh transfers refresh the listing via their
// onComplete handlers; these entries cover jobs that finish after a page reload.
const FILEBROWSER_LISTING_KEYS = [
  key("filebrowser.resource_get"),
  key("filebrowser.subfolders"),
];

const INDEXER_KEYS = [
  key("indexer.get_status"),
  key("filebrowser.indexer_status"),
  key("filebrowser.search"),
  key("filebrowser.dir_size"),
  key("filebrowser.subfolders"),
];

const UNIT_KEYS = [key("systemd.list_services"), key("systemd.get_unit_info")];

// The datetime handler prefix covers get_timezone/get_ntp_status/get_ntp_servers.
const DATETIME_KEYS: QueryKey[] = [
  ["linuxio", "datetime"],
  key("system.get_server_time"),
];

const VM_KEYS = [key("virt.list"), key("virt.get")];

/**
 * Single source of truth for which query caches a completed job makes stale,
 * keyed by route (== job type).
 *
 * Consumed from both job lifecycles:
 * - `useJobAction`/`useJobStreamAction` use the entry as the default
 *   `invalidates` for jobs started and awaited locally.
 * - `useRecoveredJobs` applies the entry when a job reaches a terminal state
 *   on the events stream with no local handler (page reload, other session),
 *   skipping jobs registered via `markJobLocallyHandled`.
 *
 * Routes without an entry invalidate nothing by default — their call sites
 * own invalidation explicitly (conditional logic, cache writes, refetches).
 */
export const ROUTE_INVALIDATIONS: Record<string, QueryKey[]> = {
  "filebrowser.index": INDEXER_KEYS,
  "filebrowser.copy_batch": FILEBROWSER_LISTING_KEYS,
  "filebrowser.move_batch": FILEBROWSER_LISTING_KEYS,
  "filebrowser.delete_batch": FILEBROWSER_LISTING_KEYS,
  "filebrowser.chmod_batch": FILEBROWSER_LISTING_KEYS,
  "filebrowser.upload": FILEBROWSER_LISTING_KEYS,
  "filebrowser.upload_batch": FILEBROWSER_LISTING_KEYS,

  "docker.start_container": CONTAINER_STATE_KEYS,
  "docker.stop_container": CONTAINER_STATE_KEYS,
  "docker.restart_container": CONTAINER_STATE_KEYS,
  "docker.start_all_stopped": CONTAINER_STATE_KEYS,
  "docker.stop_all_running": CONTAINER_STATE_KEYS,
  "docker.remove_container": CONTAINER_IMAGE_KEYS,
  "docker.update_container": CONTAINER_IMAGE_KEYS,
  "docker.check_updates": CONTAINER_IMAGE_KEYS,
  "docker.check_container_update": CONTAINER_IMAGE_KEYS,
  "docker.delete_image": [key("docker.list_images")],
  "docker.create_network": [key("docker.list_networks")],
  "docker.delete_network": [key("docker.list_networks")],
  "docker.create_volume": [key("docker.list_volumes")],
  "docker.delete_volume": [key("docker.list_volumes")],
  "docker.compose_up": COMPOSE_KEYS,
  "docker.compose_down": COMPOSE_KEYS,
  "docker.compose_stop": COMPOSE_KEYS,
  "docker.compose_restart": COMPOSE_KEYS,
  "docker.delete_stack": COMPOSE_KEYS,
  "docker.system_prune": [
    key("docker.list_containers"),
    key("docker.list_images"),
    key("docker.list_volumes"),
    key("docker.list_networks"),
  ],
  "docker.set_container_auto_update": [key("docker.get_container_auto_update")],

  "accounts.create_user": [key("accounts.list_users")],
  "accounts.delete_user": [key("accounts.list_users")],
  "accounts.modify_user": [key("accounts.list_users")],
  "accounts.lock_user": [key("accounts.list_users")],
  "accounts.unlock_user": [key("accounts.list_users")],
  "accounts.change_password": [key("accounts.list_users")],
  "accounts.create_group": [key("accounts.list_groups")],
  "accounts.delete_group": [key("accounts.list_groups")],
  "accounts.modify_group_members": [
    key("accounts.list_groups"),
    key("accounts.list_users"),
  ],
  "accounts.terminate_session": [key("accounts.get_user_details")],

  "systemd.start_service": UNIT_KEYS,
  "systemd.stop_service": UNIT_KEYS,
  "systemd.restart_service": UNIT_KEYS,
  "systemd.reload_service": UNIT_KEYS,
  "systemd.enable_service": UNIT_KEYS,
  "systemd.disable_service": UNIT_KEYS,
  "systemd.mask_service": UNIT_KEYS,
  "systemd.unmask_service": UNIT_KEYS,
  "systemd.reset_failed_service": UNIT_KEYS,

  "network.set_ipv4": [key("network.get_network_info")],
  "network.set_ipv4_manual": [key("network.get_network_info")],
  "network.enable_connection": [key("network.get_network_info")],
  "network.disable_connection": [key("network.get_network_info")],

  "hostname.set_hostname": [key("system.get_host_info")],
  "system.dismiss_unclean_shutdown": [key("system.get_health_summary")],
  "system.dismiss_failed_login_alert": [key("system.get_health_summary")],

  "datetime.set_timezone": DATETIME_KEYS,
  "datetime.set_ntp": DATETIME_KEYS,
  "datetime.set_ntp_servers": DATETIME_KEYS,
  "datetime.set_server_time": DATETIME_KEYS,

  "shares.create_nfs_share": [key("shares.list_nfs_shares")],
  "shares.update_nfs_share": [key("shares.list_nfs_shares")],
  "shares.delete_nfs_share": [key("shares.list_nfs_shares")],
  "shares.create_samba_share": [key("shares.list_samba_shares")],
  "shares.update_samba_share": [key("shares.list_samba_shares")],
  "shares.delete_samba_share": [key("shares.list_samba_shares")],

  "storage.mount_cifs": [
    key("storage.list_cifs_mounts"),
    key("system.get_fs_info"),
  ],
  "storage.unmount_cifs": [
    key("storage.list_cifs_mounts"),
    key("system.get_fs_info"),
  ],
  "storage.remount_cifs": [key("storage.list_cifs_mounts")],
  "storage.mount_nfs": [
    key("storage.list_nfs_mounts"),
    key("system.get_fs_info"),
  ],
  "storage.unmount_nfs": [
    key("storage.list_nfs_mounts"),
    key("system.get_fs_info"),
  ],
  "storage.remount_nfs": [key("storage.list_nfs_mounts")],
  "storage.create_lv": [key("storage.list_lvs"), key("storage.list_vgs")],
  "storage.resize_lv": [key("storage.list_lvs"), key("storage.list_vgs")],
  "storage.delete_lv": [key("storage.list_lvs"), key("storage.list_vgs")],
  "storage.unmount_filesystem": [
    key("storage.list_nfs_mounts"),
    key("storage.list_cifs_mounts"),
    key("system.get_fs_info"),
  ],
  "storage.run_smart_test": [key("storage.get_drive_info")],
  "storage.create_btrfs_subvolume": [key("system.get_fs_info")],

  "updates.set_auto_updates": [key("updates.get_auto_updates")],

  "wireguard.add_interface": [key("wireguard.list_interfaces")],
  "wireguard.remove_interface": [key("wireguard.list_interfaces")],
  "wireguard.up_interface": [key("wireguard.list_interfaces")],
  "wireguard.down_interface": [key("wireguard.list_interfaces")],
  "wireguard.enable_interface": [key("wireguard.list_interfaces")],
  "wireguard.disable_interface": [key("wireguard.list_interfaces")],
  "wireguard.add_peer": [
    key("wireguard.list_interfaces"),
    key("wireguard.list_peers"),
  ],
  "wireguard.remove_peer": [
    key("wireguard.list_interfaces"),
    key("wireguard.list_peers"),
  ],

  "virt.start": VM_KEYS,
  "virt.shutdown": VM_KEYS,
  "virt.reboot": VM_KEYS,
  "virt.force_off": VM_KEYS,
  "virt.suspend": VM_KEYS,
  "virt.resume": VM_KEYS,
};
