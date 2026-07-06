import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { ROUTE_MODES } from "@/api/generated/route-metadata";
import { ROUTE_INVALIDATIONS } from "@/constants/routeInvalidations";

const MODES = ROUTE_MODES as Record<string, string>;

describe("ROUTE_INVALIDATIONS", () => {
  it("maps only job routes", () => {
    for (const route of Object.keys(ROUTE_INVALIDATIONS)) {
      expect(MODES[route], `${route} is not a job route`).toBe("job");
    }
  });

  it("invalidates only real query routes or handler prefixes", () => {
    for (const [route, keys] of Object.entries(ROUTE_INVALIDATIONS)) {
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
          expect(MODES[target], `${route} -> ${target}`).toBe("query");
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
// else must declare invalidations in ROUTE_INVALIDATIONS (applied by
// useJobAction/useJobStreamAction and the recovered-jobs stream) or pass an
// explicit `invalidates` config. Shrink this list over time; never grow it
// without a reason a manifest entry cannot express.
const ALLOWED_INVALIDATE_FILES = new Set([
  // Core invalidation appliers.
  "api/react-query.ts",
  "hooks/backgroundJobs/useRecoveredJobs.ts",
  // Imperative multi-item flows still on the raw useMutation escape hatch.
  "components/navbar/IndexerSettingsSection.tsx",
  "hooks/filebrowser/useFileBrowserController.tsx",
  "hooks/filebrowser/useFileBrowserEditorActions.ts",
  "hooks/filebrowser/useFileMutations.ts",
  "pages/main/accounts/components/DeleteGroupDialog.tsx",
  "pages/main/accounts/components/DeleteUserDialog.tsx",
  "pages/main/docker/ImageList.tsx",
  "pages/main/docker/index.tsx",
  "pages/main/docker/NetworkList.tsx",
  "pages/main/docker/VolumeList.tsx",
  "pages/main/vm/CreateVMDialog.tsx",
  "pages/main/vm/index.tsx",
  "pages/main/wireguard/CreateInterfaceButton.tsx",
]);

// Vitest runs from frontend/ (the setup-file path in vitest.config depends on it).
const SRC_ROOT = join(process.cwd(), "src/");

function sourceFiles(dir: string, files: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "generated") sourceFiles(full, files);
    } else if (
      /\.(ts|tsx)$/.test(entry.name) &&
      !entry.name.includes(".test.")
    ) {
      files.push(full);
    }
  }
  return files;
}

describe("invalidation guard", () => {
  it("keeps queryClient.invalidateQueries out of feature code", () => {
    const violations = sourceFiles(SRC_ROOT)
      .filter((file) => {
        const rel = file.slice(SRC_ROOT.length).replaceAll("\\", "/");
        return (
          !ALLOWED_INVALIDATE_FILES.has(rel) &&
          readFileSync(file, "utf8").includes("invalidateQueries(")
        );
      })
      .map((file) => file.slice(SRC_ROOT.length));

    expect(
      violations,
      "Route invalidations belong in constants/routeInvalidations.ts " +
        "(applied automatically by useJobAction and the recovered-jobs stream) " +
        "or in a useJobAction `invalidates` config — not in ad-hoc " +
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
