import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { relativeToSrc, sourceFiles } from "@/test/sourceFiles";

// Feature code talks to the backend through the typed hook surface only:
// render-driven reads via `endpoint.useQuery`/`useQueries`, event-driven
// commands via `endpoint.useAction`, writes via `endpoint.useJobAction`/
// `useJobStreamAction` or the background-jobs layer, imperative loader/effect
// reads via `endpoint.useFetcher()`, and cache surgery via
// `endpoint.useCache()`. The primitives — bare `linuxio.<handler>.<command>()`
// promise calls, raw React Query hooks, and direct query-client access — live
// in the API layer, so feature code never imports @tanstack/react-query.
// See docs/api-contract.md ("Every generated endpoint exposes").

// Directories that ARE the primitive layer.
const SANCTIONED_DIR_PREFIXES = ["api/", "hooks/backgroundJobs/"];

// Beyond the primitive layer, only React Query's own wiring may import it:
// the provider, routing preload plumbing, and the test harness — plus the
// invalidation manifest's type-only `QueryKey` import.
const REACT_QUERY_IMPORT_ALLOWED_PREFIXES = [
  ...SANCTIONED_DIR_PREFIXES,
  "routing/",
  "test/",
];
const REACT_QUERY_IMPORT_ALLOWED_FILES = new Set([
  "constants/routeInvalidations.ts",
  "contexts/ReactQueryContext.tsx",
]);
const REACT_QUERY_IMPORT = /from\s*["']@tanstack\/react-query["']/;

// Files allowed to call `linuxio.<handler>.<command>(...)` directly.
// Shrink this list over time; never grow it without a structural reason.
const ALLOWED_BARE_CALL_FILES = new Set([
  // AuthProvider mounts above ReactQueryProvider, so no query client exists
  // for its capability bootstrap.
  "contexts/AuthContext.tsx",
]);

// Matches `linuxio.<handler>.<command>(` — a bare endpoint invocation. Member
// access like `linuxio.docker.list_images.useQuery(` does not match because a
// third property follows instead of a call.
const BARE_ENDPOINT_CALL = /\blinuxio\.\w+\.\w+\(/;

// Byte/mux-level transport primitives. Feature code talks streams through
// the stream openers (openTerminalStream, ...) and the lifecycle hooks; only
// the sanctioned low-level consumers below may import these from @/api.
const STREAM_PRIMITIVES = [
  "initStreamMux",
  "closeStreamMux",
  "waitForStreamMux",
  "getStreamMux",
  "encodeString",
  "decodeString",
  "configureStreamMultiplexer",
  "bindStreamHandlers",
  "waitForStreamResult",
  "streamWriteChunks",
  "createStreamMessageChannel",
  "StreamMessageChannel",
  "STREAM_MULTIPLEXER_CONFIG",
];

// Files allowed to import stream primitives: mux bootstrap, stream-lifecycle
// hooks, and a few legacy page-level consumers. Shrink this list over time;
// never grow it without a structural reason.
const STREAM_PRIMITIVE_IMPORT_ALLOWED_FILES = new Set([
  // Mux bootstrap owns init/close across sign-in/sign-out.
  "contexts/AuthContext.tsx",
  // App-update stream lifecycle (mux handle + frame decoding).
  "contexts/UpdateContext.tsx",
  // Upload chunk sizing from the transport default.
  "hooks/useUploadChunkSize.ts",
  // Stream-lifecycle hooks — the sanctioned way pages consume streams.
  "hooks/useLiveStream.ts",
  "hooks/useStreamMessageChannel.ts",
  "hooks/useStreamResult.ts",
  "hooks/useXtermStreamTerminal.ts",
  "hooks/useTerminalContextMenu.ts",
]);

const API_IMPORT_GROUP =
  /import\s+(?:type\s+)?{([^}]+)}\s*from\s*["']@\/api["']/g;

function importedStreamPrimitives(source: string): string[] {
  const found: string[] = [];
  for (const match of source.matchAll(API_IMPORT_GROUP)) {
    for (const rawName of match[1].split(",")) {
      const name = rawName
        .replace(/^\s*type\s+/, "")
        .trim()
        .split(/\s+as\s+/)[0];
      if (STREAM_PRIMITIVES.includes(name)) {
        found.push(name);
      }
    }
  }
  return found;
}

const USE_MUTATION_IMPORT =
  /import\s*(?:type\s*)?{[^}]*\buseMutation\b[^}]*}\s*from\s*["']@tanstack\/react-query["']/;

// Direct query-client fetches; feature code uses `endpoint.useFetcher()`.
const IMPERATIVE_QUERY_CLIENT_CALL = /\.(?:fetchQuery|ensureQueryData)\(/;

function isSanctioned(rel: string): boolean {
  return SANCTIONED_DIR_PREFIXES.some((prefix) => rel.startsWith(prefix));
}

describe("API layering guard", () => {
  it("keeps bare linuxio endpoint calls out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter(
        (rel) =>
          !isSanctioned(rel) &&
          !ALLOWED_BARE_CALL_FILES.has(rel) &&
          BARE_ENDPOINT_CALL.test(
            readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
          ),
      );

    expect(
      violations,
      "Feature code must not call linuxio endpoints as bare promises. " +
        "Reads go through endpoint.useQuery or " +
        "queryClient.fetchQuery(endpoint.queryOptions(...)); writes go " +
        "through endpoint.useJobAction / useJobStreamAction or the " +
        "background-jobs layer.",
    ).toEqual([]);
  });

  it("keeps direct React Query imports out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter(
        (rel) =>
          !REACT_QUERY_IMPORT_ALLOWED_PREFIXES.some((prefix) =>
            rel.startsWith(prefix),
          ) &&
          !REACT_QUERY_IMPORT_ALLOWED_FILES.has(rel) &&
          REACT_QUERY_IMPORT.test(
            readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
          ),
      );

    expect(
      violations,
      "Feature code talks to React Query through the endpoint hooks " +
        "(useQuery/useQueries/useAction/useJobAction/useJobStreamAction/" +
        "useFetcher/useCache), not by importing @tanstack/react-query.",
    ).toEqual([]);
  });

  it("keeps imperative query-client fetches out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter(
        (rel) =>
          !isSanctioned(rel) &&
          IMPERATIVE_QUERY_CLIENT_CALL.test(
            readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
          ),
      );

    expect(
      violations,
      "Feature code must not fetch through the query client directly. " +
        "Use endpoint.useFetcher() for imperative loader/effect reads and " +
        "endpoint.useAction() for event-driven commands.",
    ).toEqual([]);
  });

  it("keeps raw React Query useMutation out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter(
        (rel) =>
          !isSanctioned(rel) &&
          USE_MUTATION_IMPORT.test(
            readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
          ),
      );

    expect(
      violations,
      "Mutations belong on the typed endpoint surface " +
        "(useJobAction / useJobStreamAction), not raw useMutation.",
    ).toEqual([]);
  });

  it("keeps stream/mux primitives out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .flatMap((rel) => {
        if (isSanctioned(rel)) return [];
        if (STREAM_PRIMITIVE_IMPORT_ALLOWED_FILES.has(rel)) return [];
        const names = importedStreamPrimitives(
          readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
        );
        return names.length > 0 ? [`${rel}: ${names.join(", ")}`] : [];
      });

    expect(
      violations,
      "Feature code must not import byte/mux-level primitives from @/api. " +
        "Consume streams via the open*Stream factories and the stream " +
        "lifecycle hooks (useLiveStream/useLogStream/useStreamResult), or " +
        "add a sanctioned low-level consumer deliberately.",
    ).toEqual([]);
  });

  it("stream-primitive allowlist entries still exist and still import primitives", () => {
    const stale = [...STREAM_PRIMITIVE_IMPORT_ALLOWED_FILES].filter((rel) => {
      try {
        return (
          importedStreamPrimitives(
            readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
          ).length === 0
        );
      } catch {
        return true;
      }
    });
    expect(
      stale,
      "Remove cleaned-up files from STREAM_PRIMITIVE_IMPORT_ALLOWED_FILES",
    ).toEqual([]);
  });

  it("bare-call allowlist entries still exist and still call endpoints", () => {
    const stale = [...ALLOWED_BARE_CALL_FILES].filter((rel) => {
      try {
        return !BARE_ENDPOINT_CALL.test(
          readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
        );
      } catch {
        return true;
      }
    });
    expect(
      stale,
      "Remove cleaned-up files from ALLOWED_BARE_CALL_FILES",
    ).toEqual([]);
  });
});
