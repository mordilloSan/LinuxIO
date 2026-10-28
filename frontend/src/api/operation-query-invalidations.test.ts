import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { ROUTE_MODES } from "@/api/generated/route-metadata";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import {
  relativeToSrc,
  SRC_ROOT,
  sourceFiles,
} from "@/test/guards/sourceFiles";

const MODES = ROUTE_MODES as Record<string, string>;

describe("OPERATION_QUERY_INVALIDATIONS", () => {
  it("refreshes rsync status and service views after daemon changes", () => {
    for (const route of ["shares.save_rsync", "shares.stop_rsync"]) {
      expect(OPERATION_QUERY_INVALIDATIONS[route]).toEqual([
        ["linuxio", "shares", "get_rsync"],
        ...OPERATION_QUERY_INVALIDATIONS["systemd.restart_service"],
      ]);
    }
  });
  it("refreshes storage topology after volume and mount changes", () => {
    for (const action of [
      "create_lv",
      "resize_lv",
      "delete_lv",
      "mount_nfs",
      "unmount_nfs",
      "remount_nfs",
      "mount_cifs",
      "unmount_cifs",
      "remount_cifs",
      "unmount_filesystem",
      "btrfs_subvolume_create",
    ]) {
      expect(OPERATION_QUERY_INVALIDATIONS[`storage.${action}`]).toContainEqual(
        ["linuxio", "storage", "get_topology"],
      );
    }
  });
  it("refreshes the btrfs inventory after device, balance and scrub actions", () => {
    for (const action of [
      "device_add",
      "device_remove",
      "device_replace",
      "balance",
      "balance_cancel",
      "scrub_start",
      "scrub_cancel",
      "device_grow",
      "set_label",
    ]) {
      expect(OPERATION_QUERY_INVALIDATIONS[`storage.btrfs_${action}`]).toEqual([
        ["linuxio", "storage", "btrfs_list"],
        ["linuxio", "storage", "get_topology"],
        ["linuxio", "storage", "get_drive_info"],
      ]);
    }
  });
  it("refreshes the btrfs inventory, unmounted pools and live filesystems after pool actions", () => {
    for (const action of ["pool_create", "pool_destroy", "pool_mount"]) {
      expect(OPERATION_QUERY_INVALIDATIONS[`storage.btrfs_${action}`]).toEqual([
        ["linuxio", "storage", "btrfs_list"],
        ["linuxio", "storage", "get_topology"],
        ["linuxio", "storage", "get_drive_info"],
        ["linuxio", "storage", "btrfs_pools_offline"],
        ["linuxio", "monitoring", "get_live"],
      ]);
    }
  });
  it("refreshes the pool inventory after partition and disk actions", () => {
    for (const route of [
      "partition_create",
      "partition_format",
      "partition_delete",
      "partition_mount",
      "disk_initialize",
    ]) {
      expect(OPERATION_QUERY_INVALIDATIONS[`storage.${route}`]).toEqual(
        OPERATION_QUERY_INVALIDATIONS["storage.btrfs_pool_create"],
      );
    }
  });
  it("refreshes btrfs settings and subvolume sizes after quota and compression changes", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["storage.btrfs_set_quotas"]).toEqual([
      ["linuxio", "storage", "btrfs_settings"],
      ["linuxio", "storage", "btrfs_subvolumes"],
      ["linuxio", "storage", "btrfs_snapshots"],
    ]);
    expect(
      OPERATION_QUERY_INVALIDATIONS["storage.btrfs_set_compression"],
    ).toEqual([
      ["linuxio", "storage", "btrfs_settings"],
      ["linuxio", "storage", "btrfs_list"],
      ["linuxio", "storage", "get_topology"],
    ]);
    for (const action of ["qgroup_limit", "subvolume_set_compression"]) {
      expect(OPERATION_QUERY_INVALIDATIONS[`storage.btrfs_${action}`]).toEqual([
        ["linuxio", "storage", "btrfs_subvolumes"],
      ]);
    }
  });
  it("refreshes index-backed data after indexing", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["filebrowser.index"]).toEqual([
      ["linuxio", "indexer", "get_status"],
      ["linuxio", "filebrowser", "search"],
      ["linuxio", "filebrowser", "dir_size"],
      ["linuxio", "filebrowser", "subfolders"],
    ]);
  });

  it("refreshes the Trash listing after trash, restore and purge", () => {
    const trashList = ["linuxio", "filebrowser", "trash_list"];
    for (const route of ["trash_batch", "trash_restore", "trash_purge"]) {
      expect(
        OPERATION_QUERY_INVALIDATIONS[`filebrowser.${route}`],
      ).toContainEqual(trashList);
    }
    expect(
      OPERATION_QUERY_INVALIDATIONS["filebrowser.trash_restore"],
    ).toContainEqual(["linuxio", "filebrowser", "list_directory"]);
  });

  it("does not invalidate an open text document from file mutations", () => {
    expect(
      Object.values(OPERATION_QUERY_INVALIDATIONS)
        .flat()
        .some((queryKey) => queryKey.includes("read_text")),
    ).toBe(false);
  });

  it("refreshes only permissions after chmod", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["filebrowser.chmod_batch"]).toEqual([
      ["linuxio", "filebrowser", "resource_stat"],
    ]);
  });

  it("refreshes service, socket, and timer lists after unit actions", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["systemd.restart_service"]).toEqual([
      ["linuxio", "systemd", "list_services"],
      ["linuxio", "systemd", "list_sockets"],
      ["linuxio", "systemd", "list_timers"],
      ["linuxio", "systemd", "get_unit_info"],
    ]);
  });

  it("refreshes every compose-owned Docker summary after compose tasks", () => {
    const expected = [
      ["linuxio", "docker", "list_compose_projects"],
      ["linuxio", "docker", "list_containers"],
      ["linuxio", "docker", "list_images"],
      ["linuxio", "docker", "list_networks"],
      ["linuxio", "docker", "list_volumes"],
      ["linuxio", "docker", "get_docker_info"],
    ];

    for (const route of [
      "docker.compose",
      "docker.compose_up",
      "docker.compose_down",
      "docker.compose_stop",
      "docker.compose_restart",
      "docker.delete_stack",
    ]) {
      expect(OPERATION_QUERY_INVALIDATIONS[route]).toEqual(expected);
    }
  });

  it("refreshes Docker daemon disk usage after system prune", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["docker.system_prune"]).toEqual([
      ["linuxio", "docker", "list_containers"],
      ["linuxio", "docker", "list_images"],
      ["linuxio", "docker", "list_volumes"],
      ["linuxio", "docker", "list_networks"],
      ["linuxio", "docker", "get_docker_info"],
    ]);
  });

  it("refreshes networks and containers after network attachment changes", () => {
    const expected = [
      ["linuxio", "docker", "list_networks"],
      ["linuxio", "docker", "list_containers"],
    ];

    expect(OPERATION_QUERY_INVALIDATIONS["docker.connect_network"]).toEqual(
      expected,
    );
    expect(OPERATION_QUERY_INVALIDATIONS["docker.disconnect_network"]).toEqual(
      expected,
    );
  });

  it("refreshes container summaries and inspect data after lifecycle actions", () => {
    const stateKeys = [
      ["linuxio", "docker", "list_containers"],
      ["linuxio", "docker", "list_compose_projects"],
      ["linuxio", "docker", "inspect_container"],
    ];

    for (const route of [
      "docker.start_container",
      "docker.stop_container",
      "docker.restart_container",
      "docker.pause_container",
      "docker.unpause_container",
      "docker.kill_container",
    ]) {
      expect(OPERATION_QUERY_INVALIDATIONS[route]).toEqual(stateKeys);
    }

    expect(OPERATION_QUERY_INVALIDATIONS["docker.remove_container"]).toEqual([
      ...stateKeys,
      ["linuxio", "docker", "list_images"],
    ]);

    const configurationKeys = [
      ...stateKeys,
      ["linuxio", "docker", "list_images"],
      ["linuxio", "docker", "list_networks"],
      ["linuxio", "docker", "list_volumes"],
    ];
    expect(OPERATION_QUERY_INVALIDATIONS["docker.create_container"]).toEqual(
      configurationKeys,
    );
    expect(OPERATION_QUERY_INVALIDATIONS["docker.edit_container"]).toEqual(
      configurationKeys,
    );
  });

  it("refreshes selected account details after user mutations", () => {
    const expected = [
      ["linuxio", "accounts", "list_users"],
      ["linuxio", "accounts", "get_user_details"],
    ];

    for (const route of [
      "accounts.delete_user",
      "accounts.modify_user",
      "accounts.lock_user",
      "accounts.unlock_user",
      "accounts.change_password",
    ]) {
      expect(OPERATION_QUERY_INVALIDATIONS[route]).toEqual(expected);
    }

    expect(
      OPERATION_QUERY_INVALIDATIONS["accounts.modify_group_members"],
    ).toEqual([["linuxio", "accounts", "list_groups"], ...expected]);
  });

  it("refreshes saved VM templates after creation, download, or deletion", () => {
    for (const route of [
      "virt.create",
      "virt.template_download",
      "virt.template_delete",
    ] as const) {
      expect(OPERATION_QUERY_INVALIDATIONS[route]).toContainEqual([
        "linuxio",
        "virt",
        "templates",
      ]);
    }
  });

  it("maps only Call or Mutation routes", () => {
    for (const route of Object.keys(OPERATION_QUERY_INVALIDATIONS)) {
      expect(
        ["call", "mutation"],
        `${route} is not an operation route`,
      ).toContain(MODES[route]);
    }
  });

  it("refreshes live SMART records after a daemon refresh", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["monitoring.refresh_smart"]).toEqual([
      ["linuxio", "monitoring", "get_live"],
    ]);
  });

  it("invalidates only cacheable Call routes or handler prefixes", () => {
    for (const [route, keys] of Object.entries(OPERATION_QUERY_INVALIDATIONS)) {
      for (const queryKey of keys) {
        expect(queryKey[0], `${route}: keys must start with "linuxio"`).toBe(
          "linuxio",
        );
        expect(
          queryKey.length,
          `${route}: keys must be handler or route prefixes`,
        ).toBeGreaterThanOrEqual(2);
        expect(queryKey.length).toBeLessThanOrEqual(3);

        if (queryKey.length === 3) {
          const target = `${String(queryKey[1])}.${String(queryKey[2])}`;
          expect(["call"], `${route} -> ${target} is not cacheable`).toContain(
            MODES[target],
          );
        } else {
          const handler = String(queryKey[1]);
          expect(
            Object.keys(MODES).some((r) => r.startsWith(`${handler}.`)),
            `${route} -> unknown handler prefix "${handler}"`,
          ).toBe(true);
        }
      }
    }
  });
});

// Files allowed to call queryClient.invalidateQueries directly. Everything
// else must declare invalidations in OPERATION_QUERY_INVALIDATIONS (applied by
// useCallMutation, `.useMutation` and the
// recovered-tasks stream) or pass an explicit `invalidates` config. Shrink this
// list over time; never grow it without a reason a manifest entry cannot express.
const ALLOWED_INVALIDATE_FILES = new Set([
  // Shared mutation lifecycle and recovered Task terminal handling.
  "api/call-react-query.ts",
  "tasks/engine/useRecoveredTasks.ts",
  // File Browser owns path-precise cache invalidation that cannot be expressed
  // as a static operation manifest entry.
  "routes/_authenticated/filebrowser/-hooks/useFileBrowserEditorActions.ts",
  "routes/_authenticated/filebrowser/-hooks/useListingInvalidation.ts",
]);

describe("schedule invalidations", () => {
  it("refreshes definitions after every schedule mutation", () => {
    const expected = [
      ...OPERATION_QUERY_INVALIDATIONS["systemd.start_service"],
      ["linuxio", "schedules", "list"],
      ["linuxio", "schedules", "get"],
    ];
    for (const route of [
      "schedules.create",
      "schedules.update",
      "schedules.delete",
      "schedules.enable",
      "schedules.disable",
      "schedules.run_now",
      "schedules.stop",
    ])
      expect(OPERATION_QUERY_INVALIDATIONS[route]).toEqual(expected);
  });
});

describe("invalidation guard", () => {
  it("keeps queryClient.invalidateQueries out of feature code", () => {
    const violations = sourceFiles()
      .filter((file) => {
        const rel = relativeToSrc(file);
        return (
          !ALLOWED_INVALIDATE_FILES.has(rel) &&
          readFileSync(file, "utf8").includes("invalidateQueries(")
        );
      })
      .map(relativeToSrc);

    expect(
      violations,
      "Operation query invalidations belong in api/operation-query-invalidations.ts " +
        "(applied automatically by useCallMutation/.useMutation " +
        "and the recovered-tasks stream) " +
        "or in an action `invalidates` config — not in ad-hoc " +
        "queryClient.invalidateQueries calls.",
    ).toEqual([]);
  });

  it("allowlist entries still exist and still call invalidateQueries", () => {
    const stale = [...ALLOWED_INVALIDATE_FILES].filter((rel) => {
      try {
        return !readFileSync(join(SRC_ROOT, rel), "utf8").includes(
          "invalidateQueries(",
        );
      } catch {
        return true;
      }
    });
    expect(
      stale,
      "Remove cleaned-up files from ALLOWED_INVALIDATE_FILES",
    ).toEqual([]);
  });
});
