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

  it("maps only action or job routes", () => {
    for (const route of Object.keys(OPERATION_QUERY_INVALIDATIONS)) {
      expect(["query", "job"], `${route} is not an operation route`).toContain(
        MODES[route],
      );
    }
  });

  it("invalidates only real query routes or handler prefixes", () => {
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
// else must declare invalidations in OPERATION_QUERY_INVALIDATIONS (applied by
// useAction/useJobAction/useJobStreamAction and the recovered-jobs stream) or
// pass an explicit `invalidates` config. Shrink this list over time; never grow
// it without a reason a manifest entry cannot express.
const ALLOWED_INVALIDATE_FILES = new Set([
  // Core invalidation appliers. Feature code that needs path-precise
  // invalidation uses `endpoint.useCache().invalidate(...)` instead.
  "api/react-query.ts",
  "hooks/backgroundJobs/useRecoveredJobs.ts",
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
        "(applied automatically by useAction/useJobAction and the recovered-jobs stream) " +
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
