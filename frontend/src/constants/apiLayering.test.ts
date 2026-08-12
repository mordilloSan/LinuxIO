import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { ROUTE_MODES } from "@/api/generated/route-metadata";
import { relativeToSrc, sourceFiles } from "@/test/sourceFiles";

// Feature code talks to the backend through centralized descriptors. Migrated
// Calls pass their descriptor directly to TanStack Query or `useCallMutation`.
// Progress work uses `useTaskAction`/`useTaskStreamAction` or the
// background-tasks layer. Imperative cache reads and cache surgery use the
// QueryClient directly; transport-only work uses typed `call()`.

// Directories that ARE the primitive layer.
const SANCTIONED_DIR_PREFIXES = ["api/", "hooks/backgroundTasks/"];

// Matches `linuxio.<handler>.<command>(`. A migrated request-bearing Call uses
// exactly that syntax to create a query descriptor, so only Task
// routes are forbidden here. A Call value can no longer be invoked as a
// Promise; imperative Call transport is the separate typed `call()` function.
const BARE_ENDPOINT_CALL = /\blinuxio\.(\w+)\.(\w+)\(/g;
const ENDPOINT_QUERY_WRAPPER = /\blinuxio\.\w+\.\w+\.useQuer(?:y|ies)\s*\(/;
const LEGACY_QUERY_ENDPOINT_METHOD =
  /\blinuxio\.\w+\.\w+\.(?:queryOptions|useAction|useFetcher|useCache)\s*\(/;

function hasForbiddenBareEndpointCall(source: string): boolean {
  for (const match of source.matchAll(BARE_ENDPOINT_CALL)) {
    const route = `${match[1]}.${match[2]}` as keyof typeof ROUTE_MODES;
    if (ROUTE_MODES[route] !== "call") return true;
  }
  return false;
}

// Byte/mux-level transport primitives. Feature code opens typed Channels and
// uses the lifecycle hooks; only the sanctioned low-level consumers below may
// import these from @/api.
const STREAM_PRIMITIVES = [
  "initStreamMux",
  "closeStreamMux",
  "waitForStreamMux",
  "ensureLoaderRequestReady",
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
  // Router loader transport readiness (the only routing-level mux primitive).
  "routes/-loader.ts",
  // App-update stream lifecycle (mux handle + frame decoding).
  "contexts/UpdateProvider.tsx",
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

function isSanctioned(rel: string): boolean {
  return SANCTIONED_DIR_PREFIXES.some((prefix) => rel.startsWith(prefix));
}

describe("API layering guard", () => {
  it("keeps the Call query layer independent of Tasks", () => {
    const source = readFileSync(
      `${process.cwd()}/src/api/call-react-query.ts`,
      "utf8",
    );
    const forbidden = [
      '"./task-react-query"',
      '"./tasks"',
      "TaskEndpoint",
      "TaskSnapshot",
      "openTaskWatchStream",
      "useTaskAction",
      "useTaskStreamAction",
      "waitForTaskCompletion",
      "waitForTaskStreamAction",
      "waitForStreamResult",
    ];

    expect(
      forbidden.filter((token) => source.includes(token)),
      "Cached Call descriptors must not acquire Task lifecycle dependencies",
    ).toEqual([]);
  });

  it("opens migrated log routes directly instead of starting Tasks", () => {
    const apiSource = readFileSync(
      `${process.cwd()}/src/api/linuxio.ts`,
      "utf8",
    );
    for (const legacyOpener of [
      "openDockerLogsStream",
      "openGeneralLogsStream",
      "openServiceLogsStream",
    ]) {
      expect(apiSource).not.toContain(legacyOpener);
    }

    const directConsumers = [
      ["components/docker/LogsDialog.tsx", "docker.logs.follow"],
      [
        "routes/_authenticated/logs/-components/GeneralLogsPage.tsx",
        "logs.general.follow",
      ],
      ["components/cards/UnitLogsCard.tsx", "logs.service.follow"],
    ] as const;
    for (const [path, route] of directConsumers) {
      const source = readFileSync(`${process.cwd()}/src/${path}`, "utf8");
      expect(source).toContain(`openChannel("${route}"`);
    }
  });

  it("keeps endpoint query-hook wrappers out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter((rel) =>
        ENDPOINT_QUERY_WRAPPER.test(
          readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
        ),
      );

    expect(
      violations,
      "Render-driven reads use TanStack Query directly with a Call descriptor " +
        "rather than endpoint-owned hooks.",
    ).toEqual([]);
  });

  it("keeps removed Query endpoint methods out of source code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter((rel) =>
        LEGACY_QUERY_ENDPOINT_METHOD.test(
          readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
        ),
      );

    expect(
      violations,
      "Use Call descriptors, useCallMutation, call(), or QueryClient directly; " +
        "the generated endpoint must not own React Query methods.",
    ).toEqual([]);
  });

  it("keeps bare linuxio endpoint calls out of feature code", () => {
    const violations = sourceFiles()
      .map(relativeToSrc)
      .filter(
        (rel) =>
          !isSanctioned(rel) &&
          hasForbiddenBareEndpointCall(
            readFileSync(`${process.cwd()}/src/${rel}`, "utf8"),
          ),
      );

    expect(
      violations,
      "Feature code must not call linuxio endpoints as bare promises. " +
        "Reads go through TanStack Query descriptors; writes go through " +
        "useCallMutation / useTaskAction / " +
        "useTaskStreamAction or the " +
        "background-tasks layer.",
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
        "(useCallMutation / useTaskAction / useTaskStreamAction), " +
        "not raw useMutation.",
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
});
