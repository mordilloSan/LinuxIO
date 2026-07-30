import type { QueryKey } from "@tanstack/react-query";

import { handlerQueryPrefix, endpointQueryPrefix } from "./query-keys";

const CONTAINER_STATE_KEYS = [
  endpointQueryPrefix("docker.list_containers"),
  endpointQueryPrefix("docker.list_compose_projects"),
];
const CONTAINER_IMAGE_KEYS = [
  ...CONTAINER_STATE_KEYS,
  endpointQueryPrefix("docker.list_images"),
];
const COMPOSE_KEYS = [
  endpointQueryPrefix("docker.list_compose_projects"),
  endpointQueryPrefix("docker.list_containers"),
];

// Filebrowser listing caches. Fresh transfers refresh the listing via their
// onComplete handlers; these entries cover jobs that finish after a page reload.
const FILEBROWSER_LISTING_KEYS = [
  endpointQueryPrefix("filebrowser.resource_get"),
  endpointQueryPrefix("filebrowser.subfolders"),
];

const INDEXER_KEYS = [
  endpointQueryPrefix("indexer.get_status"),
  endpointQueryPrefix("filebrowser.indexer_status"),
  endpointQueryPrefix("filebrowser.search"),
  endpointQueryPrefix("filebrowser.dir_size"),
  endpointQueryPrefix("filebrowser.subfolders"),
];

const UNIT_KEYS = [
  endpointQueryPrefix("systemd.list_services"),
  endpointQueryPrefix("systemd.get_unit_info"),
];

// The datetime handler prefix covers get_timezone/get_ntp_status/get_ntp_servers.
const DATETIME_KEYS: QueryKey[] = [
  handlerQueryPrefix("datetime"),
  endpointQueryPrefix("system.get_server_time"),
];

const VM_KEYS = [
  endpointQueryPrefix("virt.list"),
  endpointQueryPrefix("virt.get"),
];

/**
 * Single source of truth for which query caches a completed operation makes
 * stale, keyed by route.
 *
 * Consumed from direct and job lifecycles:
 * - `useAction`/`useJobAction`/`useJobStreamAction` use the entry as the
 *   default `invalidates` for operations started and awaited locally.
 * - `useRecoveredJobs` applies the entry when a job reaches a terminal state
 *   on the events stream with no local handler (page reload, other session),
 *   skipping jobs registered via `markJobLocallyHandled`.
 *
 * Routes without an entry invalidate nothing by default — their call sites
 * own invalidation explicitly (conditional logic, cache writes, refetches).
 */
export const OPERATION_QUERY_INVALIDATIONS: Record<string, QueryKey[]> = {
  // Package-update streams may detach when the Updates layout unmounts; the
  // global jobs event owner performs this invalidation on terminal state.
  "packages.update": [endpointQueryPrefix("updates.get_updates_basic")],

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
  "docker.delete_image": [endpointQueryPrefix("docker.list_images")],
  "docker.create_network": [endpointQueryPrefix("docker.list_networks")],
  "docker.delete_network": [endpointQueryPrefix("docker.list_networks")],
  "docker.create_volume": [endpointQueryPrefix("docker.list_volumes")],
  "docker.delete_volume": [endpointQueryPrefix("docker.list_volumes")],
  "docker.compose_up": COMPOSE_KEYS,
  "docker.compose_down": COMPOSE_KEYS,
  "docker.compose_stop": COMPOSE_KEYS,
  "docker.compose_restart": COMPOSE_KEYS,
  "docker.delete_stack": COMPOSE_KEYS,
  "docker.system_prune": [
    endpointQueryPrefix("docker.list_containers"),
    endpointQueryPrefix("docker.list_images"),
    endpointQueryPrefix("docker.list_volumes"),
    endpointQueryPrefix("docker.list_networks"),
  ],
  "docker.set_container_auto_update": [
    endpointQueryPrefix("docker.get_container_auto_update"),
  ],

  "accounts.create_user": [endpointQueryPrefix("accounts.list_users")],
  "accounts.delete_user": [endpointQueryPrefix("accounts.list_users")],
  "accounts.modify_user": [endpointQueryPrefix("accounts.list_users")],
  "accounts.lock_user": [endpointQueryPrefix("accounts.list_users")],
  "accounts.unlock_user": [endpointQueryPrefix("accounts.list_users")],
  "accounts.change_password": [endpointQueryPrefix("accounts.list_users")],
  "accounts.create_group": [endpointQueryPrefix("accounts.list_groups")],
  "accounts.delete_group": [endpointQueryPrefix("accounts.list_groups")],
  "accounts.modify_group_members": [
    endpointQueryPrefix("accounts.list_groups"),
    endpointQueryPrefix("accounts.list_users"),
  ],
  "accounts.terminate_session": [
    endpointQueryPrefix("accounts.get_user_details"),
  ],

  "systemd.start_service": UNIT_KEYS,
  "systemd.stop_service": UNIT_KEYS,
  "systemd.restart_service": UNIT_KEYS,
  "systemd.reload_service": UNIT_KEYS,
  "systemd.enable_service": UNIT_KEYS,
  "systemd.disable_service": UNIT_KEYS,
  "systemd.mask_service": UNIT_KEYS,
  "systemd.unmask_service": UNIT_KEYS,
  "systemd.reset_failed_service": UNIT_KEYS,

  "network.set_ipv4": [endpointQueryPrefix("network.get_network_info")],
  "network.set_ipv4_manual": [endpointQueryPrefix("network.get_network_info")],
  "network.enable_connection": [
    endpointQueryPrefix("network.get_network_info"),
  ],
  "network.disable_connection": [
    endpointQueryPrefix("network.get_network_info"),
  ],

  "hostname.set_hostname": [endpointQueryPrefix("system.get_host_info")],
  "system.dismiss_unclean_shutdown": [
    endpointQueryPrefix("system.get_health_summary"),
  ],
  "system.dismiss_failed_login_alert": [
    endpointQueryPrefix("system.get_health_summary"),
  ],

  "datetime.set_timezone": DATETIME_KEYS,
  "datetime.set_ntp": DATETIME_KEYS,
  "datetime.set_ntp_servers": DATETIME_KEYS,
  "datetime.set_server_time": DATETIME_KEYS,

  "shares.create_nfs_share": [endpointQueryPrefix("shares.list_nfs_shares")],
  "shares.update_nfs_share": [endpointQueryPrefix("shares.list_nfs_shares")],
  "shares.delete_nfs_share": [endpointQueryPrefix("shares.list_nfs_shares")],
  "shares.create_samba_share": [
    endpointQueryPrefix("shares.list_samba_shares"),
  ],
  "shares.update_samba_share": [
    endpointQueryPrefix("shares.list_samba_shares"),
  ],
  "shares.delete_samba_share": [
    endpointQueryPrefix("shares.list_samba_shares"),
  ],

  "storage.mount_cifs": [
    endpointQueryPrefix("storage.list_cifs_mounts"),
    endpointQueryPrefix("system.get_fs_info"),
  ],
  "storage.unmount_cifs": [
    endpointQueryPrefix("storage.list_cifs_mounts"),
    endpointQueryPrefix("system.get_fs_info"),
  ],
  "storage.remount_cifs": [endpointQueryPrefix("storage.list_cifs_mounts")],
  "storage.mount_nfs": [
    endpointQueryPrefix("storage.list_nfs_mounts"),
    endpointQueryPrefix("system.get_fs_info"),
  ],
  "storage.unmount_nfs": [
    endpointQueryPrefix("storage.list_nfs_mounts"),
    endpointQueryPrefix("system.get_fs_info"),
  ],
  "storage.remount_nfs": [endpointQueryPrefix("storage.list_nfs_mounts")],
  "storage.create_lv": [
    endpointQueryPrefix("storage.list_lvs"),
    endpointQueryPrefix("storage.list_vgs"),
  ],
  "storage.resize_lv": [
    endpointQueryPrefix("storage.list_lvs"),
    endpointQueryPrefix("storage.list_vgs"),
  ],
  "storage.delete_lv": [
    endpointQueryPrefix("storage.list_lvs"),
    endpointQueryPrefix("storage.list_vgs"),
  ],
  "storage.unmount_filesystem": [
    endpointQueryPrefix("storage.list_nfs_mounts"),
    endpointQueryPrefix("storage.list_cifs_mounts"),
    endpointQueryPrefix("system.get_fs_info"),
  ],
  "storage.run_smart_test": [endpointQueryPrefix("storage.get_drive_info")],
  "storage.create_btrfs_subvolume": [endpointQueryPrefix("system.get_fs_info")],

  "updates.set_auto_updates": [endpointQueryPrefix("updates.get_auto_updates")],

  "wireguard.add_interface": [endpointQueryPrefix("wireguard.list_interfaces")],
  "wireguard.remove_interface": [
    endpointQueryPrefix("wireguard.list_interfaces"),
  ],
  "wireguard.up_interface": [endpointQueryPrefix("wireguard.list_interfaces")],
  "wireguard.down_interface": [
    endpointQueryPrefix("wireguard.list_interfaces"),
  ],
  "wireguard.enable_interface": [
    endpointQueryPrefix("wireguard.list_interfaces"),
  ],
  "wireguard.disable_interface": [
    endpointQueryPrefix("wireguard.list_interfaces"),
  ],
  "wireguard.add_peer": [
    endpointQueryPrefix("wireguard.list_interfaces"),
    endpointQueryPrefix("wireguard.list_peers"),
  ],
  "wireguard.remove_peer": [
    endpointQueryPrefix("wireguard.list_interfaces"),
    endpointQueryPrefix("wireguard.list_peers"),
  ],

  "virt.start": VM_KEYS,
  "virt.shutdown": VM_KEYS,
  "virt.reboot": VM_KEYS,
  "virt.force_off": VM_KEYS,
  "virt.suspend": VM_KEYS,
  "virt.resume": VM_KEYS,
};
