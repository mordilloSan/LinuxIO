import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";

import { describe, expect, it } from "vitest";

import { SRC_ROOT, relativeToSrc, sourceFiles } from "@/test/sourceFiles";

// The router runs one global `defaultPreload: "intent"` policy, so the route
// loader IS the prefetch: hovering a link runs the loader chain. Each suspense
// endpoint must be declared there as either critical awaited work or a deferred
// best-effort prefetch. Both paths first wait for yamux transport readiness and
// defer while an app update blocks requests; deferred widgets then own local
// Suspense and error boundaries. This guard verifies declaration coverage, not
// that conditional deferred work necessarily runs for hidden/collapsed UI.
// See docs/tanstack-router.md ("Who owns which data", and the loader /
// useSuspenseQuery shared-cache-entry section).

// Matches the two suspense observers, and only those. `useQuery`/`useQueries`
// are deliberately lazy here — dialogs, polling, progressive history charts,
// panel-gated reads (`enabled: failedLoginsOpen`) — and must never be flagged.
// The left-boundary guard keeps `endpoint.useSuspenseQuery(` and bare `import
// { useSuspenseQuery }` (no following paren) out; the `y|ies` alternation
// matches the plural form as one token, so plural sites are not counted twice.
const SUSPENSE_HOOK_CALL = /(?<![A-Za-z0-9_$.])useSuspenseQuer(?:y|ies)\s*\(/g;

// Matches generated Call descriptors and request-bound descriptor factories:
//
//   linuxio.system.get_cpu_info
//   linuxio.systemd.get_unit_info({ unit })
//
// Coverage is compared at handler.command granularity only, never on arguments
// or query keys. Route-derived critical options are single-sourced through route
// context and guarded separately in `_authenticated/-query-ownership.test.ts`;
// observers may still pass component-derived values for deliberately local work.
const ENDPOINT_QUERY_REFERENCE =
  /\blinuxio\.(\w+)\.(\w+)(?:\s*\(|(?=\s*[,)}\]]))/g;
const QUERY_OPTIONS_REFERENCE = /\b[A-Za-z_$][\w$]*QueryOptions\b/g;

// Route-context descriptors are intentionally opaque at the observer call.
// This map connects each consumer-local option name back to the endpoint that
// the route context resolves. The targeted query-ownership guard separately
// verifies that these routes construct options only in context and that their
// loaders consume `loaderArgs.context`.
const ROUTE_CONTEXT_OBSERVER_ENDPOINTS: Record<
  string,
  Record<string, string>
> = {
  "/_authenticated/accounts/": {
    listUsersQueryOptions: "accounts.list_users",
  },
  "/_authenticated/filebrowser/$": {
    listingQueryOptions: "filebrowser.resource_get",
  },
  "/_authenticated/services/": {
    listQueryOptions: "systemd.list_services",
  },
  "/_authenticated/services/sockets": {
    listQueryOptions: "systemd.list_sockets",
  },
  "/_authenticated/services/timers": {
    listQueryOptions: "systemd.list_timers",
  },
  "/_authenticated/vm/machines/$name": {
    vmQueryOptions: "virt.get",
  },
};

// `createFileRoute("<id>")` / `createRootRouteWithContext` — the router's own
// route id, preferred over deriving one from the file path.
const CREATE_FILE_ROUTE = /createFileRoute\(\s*["']([^"']+)["']/;
const ROOT_ROUTE_ID = "__root__";

// Static `from "..."` specifiers, bare side-effect imports, and the dynamic
// `import("...")` inside `lazy(...)`.
const FROM_SPECIFIER = /\bfrom\s*["']([^"']+)["']/g;
const IMPORT_OR_EXPORT_KEYWORD = /\b(?:import|export)\b/g;
const SIDE_EFFECT_IMPORT = /(?:^|[\n;])\s*import\s*["']([^"']+)["']/g;
const DYNAMIC_IMPORT = /\bimport\s*\(\s*["']([^"']+)["']\s*\)/g;

// Module-resolution order. The `/index` entries are load-bearing:
// storage/index.tsx imports `./-components/DiskOverview`, whose index.tsx is
// itself a three-endpoint suspense observer.
const RESOLVE_EXTENSIONS = ["", ".tsx", ".ts", "/index.tsx", "/index.ts"];

// The import walk is bounded to the route tree plus the shared hook layer.
// Walking src/hooks is what turns filebrowser from a blind spot into real
// coverage (hooks/filebrowser/useFileQueries.ts suspends three hops from
// filebrowser/$.tsx). src/components/** is deliberately NOT walked: see
// SHARED_SUSPENSE_OWNERS.
const WALKED_SCOPE_PREFIXES = ["routes/", "hooks/"];

// Suspense observers outside the walked scope, pinned to the routes that
// actually render them, because module-granular reachability cannot decide
// them. Shrink this list over time; never grow it without a structural reason.
const SHARED_SUSPENSE_OWNERS = [
  {
    // Reachable from five routes across three branches, but accounts/ and
    // docker/containers import only the non-suspending `DetailRow` export from
    // this module. The suspending `UnitInfoPanel` is rendered exclusively by
    // the three services tabs, which reach it through the route-local re-export
    // in services/-components/UnitViews.tsx. Attributing the module by
    // reachability would report systemd.get_unit_info against two routes that
    // never mount the observer.
    file: "components/cards/UnitInfoPanelCard.tsx",
    endpoint: "systemd.get_unit_info",
    renderedBy: [
      "/_authenticated/services/",
      "/_authenticated/services/sockets",
      "/_authenticated/services/timers",
    ],
  },
];

const OPENING_BRACKETS: Record<string, string> = {
  "(": ")",
  "[": "]",
  "{": "}",
};
const CLOSING_BRACKETS = new Set([")", "]", "}"]);
const QUOTES = new Set(['"', "'", "`"]);

function lineNumberForIndex(source: string, index: number): number {
  return source.slice(0, index).split("\n").length;
}

/** Index just past the string literal opening at `start`. */
function skipStringLiteral(code: string, start: number): number {
  const quote = code[start];
  let index = start + 1;
  while (index < code.length) {
    if (code[index] === "\\") {
      index += 2;
      continue;
    }
    if (code[index] === quote) return index + 1;
    index += 1;
  }
  return index;
}

/**
 * Blank out comment bodies while preserving every byte offset, so structural
 * scans and reported line numbers both stay aligned with the original source.
 * String-aware, so a `//` inside a URL literal is not treated as a comment.
 */
function withoutComments(source: string): string {
  const out = source.split("");
  let index = 0;
  while (index < source.length) {
    const char = source[index];
    if (QUOTES.has(char)) {
      index = skipStringLiteral(source, index);
      continue;
    }
    if (char === "/" && source[index + 1] === "/") {
      while (index < source.length && source[index] !== "\n") {
        out[index] = " ";
        index += 1;
      }
      continue;
    }
    if (char === "/" && source[index + 1] === "*") {
      const end = source.indexOf("*/", index + 2);
      const stop = end === -1 ? source.length : end + 2;
      for (let blank = index; blank < stop; blank += 1) {
        if (out[blank] !== "\n") out[blank] = " ";
      }
      index = stop;
      continue;
    }
    index += 1;
  }
  return out.join("");
}

/**
 * Text of the bracket-balanced region opening at `start`.
 *
 * This is what keeps a suspense call's argument list from bleeding into the
 * next statement. A fixed character window after `useSuspenseQuery(` runs past
 * the closing paren into adjacent lazy reads — it reports
 * system.list_failed_login_events from -dashboard/SystemHealth.tsx, whose
 * `useQuery` sits nine lines below the suspense call.
 */
function balancedRegion(code: string, start: number): string {
  let depth = 0;
  let index = start;
  while (index < code.length) {
    const char = code[index];
    if (QUOTES.has(char)) {
      index = skipStringLiteral(code, index);
      continue;
    }
    if (OPENING_BRACKETS[char]) {
      depth += 1;
      index += 1;
      continue;
    }
    if (CLOSING_BRACKETS.has(char)) {
      depth -= 1;
      index += 1;
      if (depth === 0) return code.slice(start, index);
      continue;
    }
    index += 1;
  }
  return code.slice(start);
}

/**
 * Text of an object property value starting at `start`, ending at the first
 * depth-0 comma or the enclosing object's closing brace.
 *
 * Loaders must be read as a whole region, never as a window forward from
 * `loadRouteQueries(`: five of them declare `const queries: LoaderQueryOptions[]
 * = [...]` and conditionally `queries.push(...)` BEFORE the call, so the
 * endpoints sit behind the call site. Scoping to the `loader:` value (rather
 * than the whole file) also keeps an inline route component's own observers out
 * of the warmed set — vm/machines/$name.tsx holds both.
 */
function propertyValueRegion(code: string, start: number): string {
  let depth = 0;
  let index = start;
  while (index < code.length) {
    const char = code[index];
    if (QUOTES.has(char)) {
      index = skipStringLiteral(code, index);
      continue;
    }
    if (OPENING_BRACKETS[char]) {
      depth += 1;
      index += 1;
      continue;
    }
    if (CLOSING_BRACKETS.has(char)) {
      if (depth === 0) return code.slice(start, index);
      depth -= 1;
      index += 1;
      continue;
    }
    if (char === "," && depth === 0) return code.slice(start, index);
    index += 1;
  }
  return code.slice(start);
}

function endpointsIn(text: string): string[] {
  return [...text.matchAll(ENDPOINT_QUERY_REFERENCE)].map(
    (match) => `${match[1]}.${match[2]}`,
  );
}

interface SuspenseSite {
  line: number;
  endpoints: string[];
  queryOptionReferences: string[];
}

function suspenseSitesIn(source: string, code: string): SuspenseSite[] {
  return [...code.matchAll(SUSPENSE_HOOK_CALL)].map((match) => {
    const openParen = (match.index ?? 0) + match[0].length - 1;
    const region = balancedRegion(code, openParen);
    return {
      line: lineNumberForIndex(source, match.index ?? 0),
      endpoints: [...new Set(endpointsIn(region))],
      queryOptionReferences: [
        ...new Set(
          [...region.matchAll(QUERY_OPTIONS_REFERENCE)].map(
            (reference) => reference[0],
          ),
        ),
      ],
    };
  });
}

function loaderEndpointsIn(code: string): string[] {
  const found: string[] = [];
  for (const match of code.matchAll(/\bloader:\s*/g)) {
    const start = (match.index ?? 0) + match[0].length;
    found.push(...endpointsIn(propertyValueRegion(code, start)));
  }
  return found;
}

function routeContextEndpointsIn(code: string): string[] {
  const found: string[] = [];
  for (const match of code.matchAll(/\bcontext:\s*/g)) {
    const start = (match.index ?? 0) + match[0].length;
    found.push(...endpointsIn(propertyValueRegion(code, start)));
  }
  return found;
}

/**
 * Import specifiers whose module is part of the rendered component graph.
 *
 * Type-only statements are skipped. That single rule is what stops the graph
 * from collapsing: two shared modules `import type { FileRouteTypes } from
 * "@/routeTree.gen"`, and routeTree.gen statically imports every route module,
 * so following that edge would make every route reach every component.
 */
function importSpecifiersIn(code: string): string[] {
  const specifiers = new Set<string>();
  const keywordOffsets = [...code.matchAll(IMPORT_OR_EXPORT_KEYWORD)].map(
    (match) => match.index ?? 0,
  );

  for (const match of code.matchAll(FROM_SPECIFIER)) {
    const fromIndex = match.index ?? 0;
    let keywordIndex = -1;
    for (const offset of keywordOffsets) {
      if (offset >= fromIndex) break;
      keywordIndex = offset;
    }
    if (keywordIndex === -1) continue;

    // `import` and `export` are both six characters.
    const clause = code.slice(keywordIndex + 6, fromIndex);
    // A statement terminator inside the clause means the nearest keyword
    // belongs to an earlier statement, so this is not an import at all.
    if (clause.includes(";")) continue;
    if (/^\s*type\s/.test(clause)) continue;
    const braced = clause.match(/\{([\s\S]*)\}/);
    if (braced) {
      const names = braced[1]
        .split(",")
        .map((name) => name.trim())
        .filter(Boolean);
      if (names.length > 0 && names.every((name) => /^type\s/.test(name))) {
        continue;
      }
    }
    specifiers.add(match[1]);
  }

  for (const match of code.matchAll(SIDE_EFFECT_IMPORT))
    specifiers.add(match[1]);
  for (const match of code.matchAll(DYNAMIC_IMPORT)) specifiers.add(match[1]);

  return [...specifiers];
}

const allSourceFiles = new Set(sourceFiles());

function resolveImport(fromFile: string, specifier: string): string | null {
  let base: string;
  if (specifier.startsWith("@/")) base = join(SRC_ROOT, specifier.slice(2));
  else if (specifier.startsWith(".")) base = join(dirname(fromFile), specifier);
  else return null;

  for (const extension of RESOLVE_EXTENSIONS) {
    const candidate = base + extension;
    if (allSourceFiles.has(candidate)) return candidate;
  }
  return null;
}

interface ModuleFacts {
  source: string;
  code: string;
  imports: string[];
  suspenseSites: SuspenseSite[];
}

const moduleCache = new Map<string, ModuleFacts>();

function readModule(file: string): ModuleFacts {
  const cached = moduleCache.get(file);
  if (cached) return cached;
  const source = readFileSync(file, "utf8");
  const code = withoutComments(source);
  const facts: ModuleFacts = {
    source,
    code,
    imports: importSpecifiersIn(code),
    suspenseSites: suspenseSitesIn(source, code),
  };
  moduleCache.set(file, facts);
  return facts;
}

// Route modules: everything under src/routes except `-` prefixed files and
// directories, which the TanStack Router plugin ignores (that is also why this
// test file is `-` prefixed).
const routeFiles = sourceFiles(join(SRC_ROOT, "routes")).filter(
  (file) =>
    !relativeToSrc(file)
      .split("/")
      .some((segment) => segment.startsWith("-")),
);
const routeFileSet = new Set(routeFiles);

interface RouteNode {
  file: string;
  id: string;
  loaderEndpoints: string[];
}

const routes: RouteNode[] = routeFiles.map((file) => {
  const { code } = readModule(file);
  const match = code.match(CREATE_FILE_ROUTE);
  const id = match ? match[1] : ROOT_ROUTE_ID;
  return {
    file,
    id,
    loaderEndpoints: [
      ...loaderEndpointsIn(code),
      ...routeContextEndpointsIn(code),
      ...Object.values(ROUTE_CONTEXT_OBSERVER_ENDPOINTS[id] ?? {}),
    ],
  };
});
const routesById = new Map(routes.map((route) => [route.id, route]));

/**
 * The route's parent, matching the generated `parentRoute` map exactly.
 *
 * Two rules are non-obvious and both are required. An index route id ends in
 * `/` and never parents anything, and an index route's parent is the SAME-PATH
 * layout route: the parent of `/_authenticated/vm/` is `/_authenticated/vm`
 * (vm/route.tsx), not `/_authenticated`. Stripping the last segment instead
 * skips the layout loader and reports bogus virt.list/virt.preflight misses
 * across the whole vm branch.
 */
function parentRouteOf(route: RouteNode): RouteNode | undefined {
  if (route.id === ROOT_ROUTE_ID) return undefined;

  if (route.id.endsWith("/")) {
    const samePathLayout = routesById.get(route.id.slice(0, -1));
    if (samePathLayout) return samePathLayout;
  }

  let best: RouteNode | undefined;
  for (const candidate of routes) {
    if (candidate === route) continue;
    if (candidate.id === ROOT_ROUTE_ID || candidate.id.endsWith("/")) continue;
    if (!route.id.startsWith(`${candidate.id}/`)) continue;
    if (!best || candidate.id.length > best.id.length) best = candidate;
  }
  return best ?? routesById.get(ROOT_ROUTE_ID);
}

const parentIdByRouteId = new Map(
  routes.map((route) => [route.id, parentRouteOf(route)?.id]),
);

/** Endpoints declared for a route: its own loader plus every ancestor's. */
function warmedEndpointsFor(route: RouteNode): {
  warmed: Set<string>;
  chain: string[];
} {
  const warmed = new Set<string>();
  const chain: string[] = [];
  let current: RouteNode | undefined = route;
  while (current) {
    chain.push(current.id);
    for (const endpoint of current.loaderEndpoints) warmed.add(endpoint);
    const parentId = parentIdByRouteId.get(current.id);
    current = parentId ? routesById.get(parentId) : undefined;
  }
  return { warmed, chain };
}

function isInWalkedScope(file: string): boolean {
  const rel = relativeToSrc(file);
  if (rel === "routeTree.gen.ts") return false;
  return WALKED_SCOPE_PREFIXES.some((prefix) => rel.startsWith(prefix));
}

/**
 * Modules rendered by a route, with the import chain that reached each one.
 *
 * The walk never crosses into another route module: children render through
 * `<Outlet/>` and own their own loader chain, so traversing route to route both
 * over-attributes and re-opens the routeTree.gen collapse. A `seen` set is the
 * only cycle handling needed.
 */
function reachableModules(routeFile: string): Map<string, string[]> {
  const chains = new Map<string, string[]>([
    [routeFile, [relativeToSrc(routeFile)]],
  ]);
  const stack = [routeFile];
  while (stack.length > 0) {
    const file = stack.pop() as string;
    for (const specifier of readModule(file).imports) {
      const target = resolveImport(file, specifier);
      if (!target || chains.has(target)) continue;
      if (!isInWalkedScope(target)) continue;
      if (target !== routeFile && routeFileSet.has(target)) continue;
      chains.set(target, [
        ...(chains.get(file) as string[]),
        relativeToSrc(target),
      ]);
      stack.push(target);
    }
  }
  return chains;
}

interface CoverageScan {
  violations: string[];
  observerSites: Set<string>;
  endpointChecks: number;
  modulesWalked: Set<string>;
  emptySites: string[];
}

function scanRouteCoverage(): CoverageScan {
  const violations: string[] = [];
  const observerSites = new Set<string>();
  const modulesWalked = new Set<string>();
  const emptySites = new Set<string>();
  let endpointChecks = 0;

  for (const route of routes) {
    const { warmed, chain } = warmedEndpointsFor(route);
    const chains = reachableModules(route.file);

    for (const [file, importChain] of chains) {
      modulesWalked.add(relativeToSrc(file));
      for (const site of readModule(file).suspenseSites) {
        const at = `${relativeToSrc(file)}:${site.line}`;
        observerSites.add(at);
        const endpoints = new Set(site.endpoints);
        const contextEndpoints = ROUTE_CONTEXT_OBSERVER_ENDPOINTS[route.id];
        for (const reference of site.queryOptionReferences) {
          const endpoint = contextEndpoints?.[reference];
          if (endpoint) endpoints.add(endpoint);
        }
        if (endpoints.size === 0) emptySites.add(`${at} for ${route.id}`);

        for (const endpoint of endpoints) {
          endpointChecks += 1;
          if (warmed.has(endpoint)) continue;
          violations.push(
            `${at} suspends on ${endpoint} for route ${route.id} — add ` +
              `${endpoint}'s descriptor to the loader for ${route.id}, ` +
              `or switch to useQuery if it is intentionally lazy ` +
              `(loader chain: ${chain.join(" <- ")}; imported via ` +
              `${importChain.join(" -> ")})`,
          );
        }
      }
    }
  }

  return {
    violations,
    observerSites,
    endpointChecks,
    modulesWalked,
    emptySites: [...emptySites],
  };
}

const coverage = scanRouteCoverage();

/** Every suspense observer in the tree, regardless of reachability. */
const suspenseFilesInTree = sourceFiles().flatMap((file) => {
  const { suspenseSites } = readModule(file);
  return suspenseSites.length > 0
    ? [{ rel: relativeToSrc(file), sites: suspenseSites }]
    : [];
});

describe("suspense loader coverage guard", () => {
  it("declares every suspense endpoint in a loader in that route's branch", () => {
    expect(
      coverage.violations,
      "Every endpoint read with useSuspenseQuery/useSuspenseQueries must be " +
        "declared by critical loading or deferred prefetch in a loader on " +
        "that route or an ancestor. Add the endpoint to the loader named in " +
        "each entry, or make the read a lazy useQuery.",
    ).toEqual([]);
  });

  it("reaches every suspense observer from a route or a pinned owner", () => {
    const pinnedFiles = new Set(
      SHARED_SUSPENSE_OWNERS.map((owner) => owner.file),
    );
    const unaccounted = suspenseFilesInTree
      .filter(({ rel }) => !pinnedFiles.has(rel))
      .filter(({ rel, sites }) => {
        if (!WALKED_SCOPE_PREFIXES.some((prefix) => rel.startsWith(prefix))) {
          return true;
        }
        return sites.some(
          (site) => !coverage.observerSites.has(`${rel}:${site.line}`),
        );
      })
      .map(({ rel }) => rel);

    expect(
      unaccounted,
      "A suspense observer was added where no route reaches it. Move it " +
        "under src/routes or src/hooks so the import walk attributes it to a " +
        "route, or pin it in SHARED_SUSPENSE_OWNERS with the routes that " +
        "render it.",
    ).toEqual([]);
  });

  it("resolves an endpoint literal inside every suspense call", () => {
    expect(
      coverage.emptySites,
      "Every suspense call must expose a literal linuxio descriptor or a " +
        "route-context option name registered in " +
        "ROUTE_CONTEXT_OBSERVER_ENDPOINTS. Otherwise the coverage scan cannot " +
        "prove that its route loader warms the same endpoint.",
    ).toEqual([]);
  });

  it("warms the pinned shared observers from every route that renders them", () => {
    const stale = SHARED_SUSPENSE_OWNERS.flatMap((owner) => {
      const pinned = suspenseFilesInTree.find(
        (entry) => entry.rel === owner.file,
      );
      if (!pinned) return [`${owner.file}: no suspense observer left`];
      if (
        !pinned.sites.some((site) => site.endpoints.includes(owner.endpoint))
      ) {
        return [`${owner.file}: no longer suspends on ${owner.endpoint}`];
      }
      return owner.renderedBy.flatMap((routeId) => {
        const route = routesById.get(routeId);
        if (!route) return [`${owner.file}: unknown route ${routeId}`];
        return warmedEndpointsFor(route).warmed.has(owner.endpoint)
          ? []
          : [
              `${owner.file}:${owner.endpoint} is not warmed for route ` +
                `${routeId} — add ${owner.endpoint}'s descriptor to the ` +
                `loader for ${routeId}, or switch to useQuery if it is ` +
                `intentionally lazy`,
            ];
      });
    });

    expect(
      stale,
      "Fix or remove the affected SHARED_SUSPENSE_OWNERS entry",
    ).toEqual([]);
  });

  it("discovers the whole route tree and its rendered modules", () => {
    // Numbers this small would mean discovery broke (wrong cwd, unresolved
    // alias, a glob that stopped matching) rather than that routes, modules or
    // suspense reads were deleted. Baselines when written: 40 routes, 238
    // modules, 52 reachable observer sites, 75 endpoint checks, 53 observer
    // sites tree-wide, 45 distinct suspense endpoints.
    expect(routes.length).toBeGreaterThan(35);
    expect(coverage.modulesWalked.size).toBeGreaterThan(200);
    expect(coverage.observerSites.size).toBeGreaterThan(45);
    expect(coverage.endpointChecks).toBeGreaterThan(60);
    expect(
      suspenseFilesInTree.reduce((total, { sites }) => total + sites.length, 0),
    ).toBeGreaterThan(45);
    expect(
      new Set([
        ...suspenseFilesInTree.flatMap(({ sites }) =>
          sites.flatMap((site) => site.endpoints),
        ),
        ...Object.values(ROUTE_CONTEXT_OBSERVER_ENDPOINTS).flatMap(
          (endpoints) => Object.values(endpoints),
        ),
      ]).size,
    ).toBeGreaterThan(40);

    // Every route resolves a parent except the root, and the loader chain is
    // walked rather than a single route's loader.
    expect(
      routes
        .filter((route) => route.id !== ROOT_ROUTE_ID)
        .filter((route) => !parentIdByRouteId.get(route.id))
        .map((route) => route.id),
    ).toEqual([]);
    expect(
      warmedEndpointsFor(
        routesById.get("/_authenticated/vm/images") as RouteNode,
      ).warmed.has("virt.preflight"),
    ).toBe(true);
  });
});
