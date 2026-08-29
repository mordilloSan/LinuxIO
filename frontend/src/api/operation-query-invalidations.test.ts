import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { ROUTE_MODES } from "@/api/generated/route-metadata";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import { SRC_ROOT, relativeToSrc, sourceFiles } from "@/test/sourceFiles";

const MODES = ROUTE_MODES as Record<string, string>;

describe("OPERATION_QUERY_INVALIDATIONS", () => {
  it("refreshes index-backed data after indexing", () => {
    expect(OPERATION_QUERY_INVALIDATIONS["filebrowser.index"]).toEqual([
      ["linuxio", "indexer", "get_status"],
      ["linuxio", "filebrowser", "indexer_status"],
      ["linuxio", "filebrowser", "search"],
      ["linuxio", "filebrowser", "dir_size"],
      ["linuxio", "filebrowser", "subfolders"],
    ]);
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

  it("maps only Call or Task routes", () => {
    for (const route of Object.keys(OPERATION_QUERY_INVALIDATIONS)) {
      expect(["call", "task"], `${route} is not an operation route`).toContain(
        MODES[route],
      );
    }
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
// useCallMutation/useTaskAction/useTaskStreamAction and the
// recovered-tasks stream) or pass an explicit `invalidates` config. Shrink this
// list over time; never grow it without a reason a manifest entry cannot express.
const ALLOWED_INVALIDATE_FILES = new Set([
  // Shared mutation lifecycle and recovered Task terminal handling.
  "api/call-react-query.ts",
  "hooks/backgroundTasks/useRecoveredTasks.ts",
  // File Browser owns path-precise cache invalidation that cannot be expressed
  // as a static operation manifest entry.
  "hooks/filebrowser/useFileBrowserEditorActions.ts",
  "hooks/filebrowser/useListingInvalidation.ts",
]);

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
        "(applied automatically by useCallMutation/useTaskAction " +
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
