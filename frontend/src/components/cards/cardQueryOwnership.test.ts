import { readFileSync } from "node:fs";
import path from "node:path";

import ts from "typescript";
import { describe, expect, it } from "vitest";

const sourceRoot = path.resolve(process.cwd(), "src");

const queryHooks = [
  "useQuery",
  "useQueries",
  "useSuspenseQuery",
  "useSuspenseQueries",
] as const;

type QueryHook = (typeof queryHooks)[number];
type NamedFunction =
  | ts.ArrowFunction
  | ts.FunctionDeclaration
  | ts.FunctionExpression;

interface ParsedSource {
  file: string;
  sourceFile: ts.SourceFile;
}

interface FunctionSource extends ParsedSource {
  body: ts.ConciseBody;
  node: NamedFunction;
}

interface ComponentRef {
  file: string;
  name: string;
}

interface HookContract extends ComponentRef {
  hooks?: Partial<Record<QueryHook, number>>;
}

interface SelectContract extends ComponentRef {
  hook: QueryHook;
  memoized?: boolean;
  select: string;
}

interface EdgeContract extends ComponentRef {
  renders: string[];
}

const parsedSources = new Map<string, ParsedSource>();

const parseSource = (relativeFile: string): ParsedSource => {
  const existing = parsedSources.get(relativeFile);
  if (existing) return existing;

  const file = path.join(sourceRoot, relativeFile);
  const source = readFileSync(file, "utf8");
  const parsed = {
    file,
    sourceFile: ts.createSourceFile(
      file,
      source,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TSX,
    ),
  };
  parsedSources.set(relativeFile, parsed);
  return parsed;
};

const functionFromVariable = (
  declaration: ts.VariableDeclaration,
): NamedFunction | null => {
  const initializer = declaration.initializer;
  return initializer &&
    (ts.isArrowFunction(initializer) || ts.isFunctionExpression(initializer))
    ? initializer
    : null;
};

const findNamedFunction = ({ file, name }: ComponentRef): FunctionSource => {
  const parsed = parseSource(file);
  const matches: NamedFunction[] = [];

  for (const statement of parsed.sourceFile.statements) {
    if (ts.isFunctionDeclaration(statement) && statement.name?.text === name) {
      matches.push(statement);
      continue;
    }
    if (!ts.isVariableStatement(statement)) continue;
    for (const declaration of statement.declarationList.declarations) {
      if (
        !ts.isIdentifier(declaration.name) ||
        declaration.name.text !== name
      ) {
        continue;
      }
      const functionNode = functionFromVariable(declaration);
      if (functionNode) matches.push(functionNode);
    }
  }

  if (matches.length !== 1) {
    throw new Error(
      `${file}: expected exactly one function named ${name}, found ${matches.length}`,
    );
  }
  const node = matches[0];
  if (!node.body) {
    throw new Error(`${file}: function ${name} has no implementation body`);
  }
  return { ...parsed, body: node.body, node };
};

const isNestedFunction = (node: ts.Node): boolean =>
  ts.isArrowFunction(node) ||
  ts.isFunctionDeclaration(node) ||
  ts.isFunctionExpression(node);

const directCalls = (
  component: FunctionSource,
  names: ReadonlySet<string>,
): ts.CallExpression[] => {
  const calls: ts.CallExpression[] = [];
  const visit = (node: ts.Node) => {
    if (node !== component.body && isNestedFunction(node)) return;
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      names.has(node.expression.text)
    ) {
      calls.push(node);
    }
    ts.forEachChild(node, visit);
  };
  visit(component.body);
  return calls;
};

const directQueryCalls = (component: FunctionSource) =>
  directCalls(component, new Set<string>(queryHooks));

const hookCounts = (component: FunctionSource): Record<QueryHook, number> => {
  const counts = Object.fromEntries(
    queryHooks.map((hook) => [hook, 0]),
  ) as Record<QueryHook, number>;
  for (const call of directQueryCalls(component)) {
    const hook = (call.expression as ts.Identifier).text as QueryHook;
    counts[hook] += 1;
  }
  return counts;
};

const returnedExpressions = (component: FunctionSource): ts.Expression[] => {
  if (!ts.isBlock(component.body)) return [component.body];

  const expressions: ts.Expression[] = [];
  const visit = (node: ts.Node) => {
    if (node !== component.body && isNestedFunction(node)) return;
    if (ts.isReturnStatement(node) && node.expression) {
      expressions.push(node.expression);
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(component.body);
  return expressions;
};

const renderedJsxNames = (component: FunctionSource): Set<string> => {
  const names = new Set<string>();
  const visit = (node: ts.Node) => {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      names.add(node.tagName.getText(component.sourceFile));
    }
    ts.forEachChild(node, visit);
  };
  for (const expression of returnedExpressions(component)) visit(expression);
  return names;
};

const renderedJsxValues = (component: FunctionSource): Set<string> => {
  const names = new Set<string>();
  const visit = (node: ts.Node) => {
    if (
      ts.isJsxExpression(node) &&
      node.expression &&
      ts.isIdentifier(node.expression)
    ) {
      names.add(node.expression.text);
    }
    ts.forEachChild(node, visit);
  };
  for (const expression of returnedExpressions(component)) visit(expression);
  return names;
};

const dashboardCardFile = "components/cards/DashboardCard.tsx";
const dashboardRoute = (file: string) =>
  `routes/_authenticated/-dashboard/${file}.tsx`;
const dockerContainerCardFile = "components/cards/ContainerCard.tsx";
const dockerContainerListFile =
  "routes/_authenticated/docker/-components/ContainerList.tsx";

const hookContracts: HookContract[] = [
  // Shared dashboard shell and its query-free owners.
  { file: dashboardCardFile, name: "DashboardCard" },
  { file: dashboardRoute("SystemHealth"), name: "SystemHealth" },
  { file: dashboardRoute("Docker"), name: "DockerInfo" },
  { file: dashboardRoute("Network"), name: "NetworkInterfacesCard" },
  { file: dashboardRoute("MotherBoard"), name: "MotherBoardInfo" },
  { file: dashboardRoute("Processor"), name: "Processor" },
  { file: dashboardRoute("Memory"), name: "MemoryUsage" },
  { file: dashboardRoute("FileSystem"), name: "FsInfoCard" },
  { file: dashboardRoute("Gpu"), name: "GpuInfo" },
  {
    file: dashboardRoute("Drive"),
    name: "Drive",
    hooks: { useSuspenseQuery: 1 },
  },
  {
    file: dashboardRoute("SystemOverview"),
    name: "SystemOverview",
    hooks: { useSuspenseQuery: 1 },
  },
  {
    file: dashboardRoute("MotherBoard"),
    name: "MotherboardTempBadge",
    hooks: { useQuery: 1 },
  },
  {
    file: dashboardRoute("Processor"),
    name: "CpuTempBadge",
    hooks: { useSuspenseQuery: 1 },
  },
  ...["DriveSelect", "DriveStats"].map((name) => ({
    file: dashboardRoute("Drive"),
    name,
    hooks: { useSuspenseQuery: 1 },
  })),
  {
    file: dashboardRoute("Drive"),
    name: "DriveGraphPane",
    hooks: { useSuspenseQueries: 1 },
  },
  ...["NetworkHeader", "NetworkStats", "NetworkGraphPane"].map((name) => ({
    file: dashboardRoute("Network"),
    name,
    hooks: { useSuspenseQuery: 1 },
  })),

  // Docker card identities, query-free shell, and live cache body.
  {
    file: dockerContainerListFile,
    name: "ContainerList",
    hooks: { useSuspenseQuery: 1 },
  },
  { file: dockerContainerCardFile, name: "ContainerCard" },
  {
    file: dockerContainerCardFile,
    name: "ContainerCardLive",
    hooks: { useQuery: 1 },
  },
  { file: dockerContainerCardFile, name: "ContainerCardBody" },

  // Network interface identities, stable card shell, and live cache slots.
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceList",
    hooks: { useSuspenseQuery: 1 },
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceTrafficGraphs",
    hooks: { useQuery: 1 },
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceConfigurationCards",
    hooks: { useQuery: 1 },
  },
  {
    file: "components/cards/NetworkInterfaceCard.tsx",
    name: "NetworkInterfaceCard",
  },
  {
    file: "components/cards/NetworkInterfaceCard.tsx",
    name: "NetworkInterfaceCardContent",
    hooks: { useQuery: 1 },
  },

  // WireGuard list polling, query-free card, and shared selected observers.
  {
    file: "routes/_authenticated/wireguard/-components/InterfaceClients.tsx",
    name: "InterfaceClients",
    hooks: { useQuery: 2 },
  },
  { file: "components/cards/WireguardPeerCard.tsx", name: "WireguardPeerCard" },
  {
    file: "components/cards/WireguardPeerCard.tsx",
    name: "WireguardPeerStatus",
  },
  {
    file: "components/cards/WireguardPeerCard.tsx",
    name: "WireguardPeerStats",
  },
  {
    file: "components/cards/WireguardPeerCard.tsx",
    name: "usePeer",
    hooks: { useQuery: 1 },
  },

  // Hardware sensor identity owner, shell, and live readings.
  {
    file: "routes/_authenticated/hardware/-components/HardwarePage.tsx",
    name: "SensorReadings",
    hooks: { useSuspenseQuery: 1 },
  },
  {
    file: "components/cards/SensorGroupCard.tsx",
    name: "SensorGroupCardShell",
  },
  {
    file: "components/cards/SensorGroupCard.tsx",
    name: "SensorGroupCardLive",
    hooks: { useQuery: 1 },
  },

  // Shared history chrome and the four independently polling hardware bodies.
  {
    file: "components/charts/HistoryCard.tsx",
    name: "HistoryCardShell",
  },
  {
    file: "components/charts/HistoryCard.tsx",
    name: "HistoryCardBody",
  },
  ...[
    "CPUHistoryCard",
    "MemoryHistoryCard",
    "DiskIOHistoryCard",
    "NetworkHistoryCard",
  ].map((name) => ({
    file: "routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx",
    name,
  })),
  ...[
    "CPUHistoryLive",
    "MemoryHistoryLive",
    "DiskIOLive",
    "NetworkHistoryLive",
  ].map((name) => ({
    file: "routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx",
    name,
    hooks: { useQuery: 1 },
  })),

  // Network detail cards: the interface list owns the polling, each card
  // observes the one interface it renders.
  {
    file: "routes/_authenticated/network/-components/NetworkTrafficHistoryCard.tsx",
    name: "NetworkTrafficHistoryCard",
  },
  {
    file: "routes/_authenticated/network/-components/NetworkTrafficHistoryCard.tsx",
    name: "NetworkTrafficHistoryLive",
    hooks: { useQuery: 1 },
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceStatsCard.tsx",
    name: "NetworkInterfaceStatsCard",
    hooks: { useQuery: 1 },
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceLogsCard.tsx",
    name: "NetworkInterfaceLogsCard",
    hooks: { useQuery: 1 },
  },

  // Active mount-list view polling (NFS and SMB share one generic list) and
  // the selected card cache observer.
  {
    file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
    name: "ProtocolMountList",
  },
  {
    file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
    name: "MountCardGrid",
    hooks: { useSuspenseQuery: 1 },
  },
  {
    file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
    name: "MountTable",
    hooks: { useSuspenseQuery: 1 },
  },
  { file: "components/cards/MountCard.tsx", name: "MountCard" },
  {
    file: "components/cards/MountCard.tsx",
    name: "MountCardLiveContent",
    hooks: { useQuery: 1 },
  },

  // Standalone unit panel shell and its sole polling body.
  { file: "components/cards/UnitInfoPanelCard.tsx", name: "UnitInfoPanel" },
  {
    file: "components/cards/UnitInfoPanelCard.tsx",
    name: "UnitInfoPanelLive",
    hooks: { useSuspenseQuery: 1 },
  },
];

const selectContracts: SelectContract[] = [
  {
    file: dashboardRoute("MotherBoard"),
    name: "MotherboardTempBadge",
    hook: "useQuery",
    memoized: true,
    select: "selectBadge",
  },
  {
    file: dashboardRoute("Processor"),
    name: "CpuTempBadge",
    hook: "useSuspenseQuery",
    memoized: true,
    select: "selectBadge",
  },
  {
    file: dashboardRoute("Drive"),
    name: "DriveSelect",
    hook: "useSuspenseQuery",
    memoized: true,
    select: "selectHeader",
  },
  {
    file: dashboardRoute("Drive"),
    name: "DriveStats",
    hook: "useSuspenseQuery",
    memoized: true,
    select: "selectDrive",
  },
  {
    file: dashboardRoute("Drive"),
    name: "DriveGraphPane",
    hook: "useSuspenseQueries",
    memoized: true,
    select: "selectDriveName",
  },
  {
    file: dashboardRoute("Network"),
    name: "NetworkHeader",
    hook: "useSuspenseQuery",
    memoized: true,
    select: "selectHeader",
  },
  {
    file: dashboardRoute("Network"),
    name: "NetworkStats",
    hook: "useSuspenseQuery",
    memoized: true,
    select: "selectDetails",
  },
  {
    file: dashboardRoute("Network"),
    name: "NetworkGraphPane",
    hook: "useSuspenseQuery",
    memoized: true,
    select: "selectThroughput",
  },
  {
    file: dashboardRoute("Drive"),
    name: "Drive",
    hook: "useSuspenseQuery",
    select: "hasAnyDrive",
  },
  {
    file: dashboardRoute("SystemOverview"),
    name: "SystemOverview",
    hook: "useSuspenseQuery",
    select: "selectPlatform",
  },
  {
    file: dockerContainerListFile,
    name: "ContainerList",
    hook: "useSuspenseQuery",
    select: "selectContainers",
  },
  {
    file: dockerContainerCardFile,
    name: "ContainerCardLive",
    hook: "useQuery",
    memoized: true,
    select: "selectContainer",
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceList",
    hook: "useSuspenseQuery",
    select: "selectNetworkInterfaceIdentities",
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceTrafficGraphs",
    hook: "useQuery",
    select: "selectNetworkInterface",
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceConfigurationCards",
    hook: "useQuery",
    select: "selectNetworkInterface",
  },
  {
    file: "components/cards/NetworkInterfaceCard.tsx",
    name: "NetworkInterfaceCardContent",
    hook: "useQuery",
    select: "selectNetworkInterface",
  },
  {
    file: "routes/_authenticated/wireguard/-components/InterfaceClients.tsx",
    name: "InterfaceClients",
    hook: "useQuery",
    select: "selectPeerIdentities",
  },
  {
    file: "components/cards/WireguardPeerCard.tsx",
    name: "usePeer",
    hook: "useQuery",
    select: "selectPeer",
  },
  {
    file: "routes/_authenticated/hardware/-components/HardwarePage.tsx",
    name: "SensorReadings",
    hook: "useSuspenseQuery",
    select: "selectVisibleSensorGroupIdentities",
  },
  {
    file: "components/cards/SensorGroupCard.tsx",
    name: "SensorGroupCardLive",
    hook: "useQuery",
    select: "selectGroup",
  },
  {
    file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
    name: "MountCardGrid",
    hook: "useSuspenseQuery",
    select: "selectMountIdentities",
  },
  {
    file: "components/cards/MountCard.tsx",
    name: "MountCardLiveContent",
    hook: "useQuery",
    select: "selectMount",
  },
];

const edgeContracts: EdgeContract[] = [
  { file: dashboardCardFile, name: "DashboardCard", renders: ["FrostedCard"] },
  ...[
    ["SystemHealth", "SystemHealth"],
    ["Docker", "DockerInfo"],
    ["Network", "NetworkInterfacesCard"],
    ["MotherBoard", "MotherBoardInfo"],
    ["Processor", "Processor"],
    ["Memory", "MemoryUsage"],
    ["FileSystem", "FsInfoCard"],
    ["Gpu", "GpuInfo"],
    ["Drive", "Drive"],
    ["SystemOverview", "SystemOverview"],
  ].map(([file, name]) => ({
    file: dashboardRoute(file),
    name,
    renders: ["DashboardCard"],
  })),
  {
    file: dockerContainerListFile,
    name: "ContainerList",
    renders: ["SelectedContainerDetails"],
  },
  {
    file: dockerContainerListFile,
    name: "SelectedContainerDetails",
    renders: ["ContainerResourceDetails"],
  },
  {
    file: "components/docker/ContainerResourceDetails.tsx",
    name: "ContainerResourceDetails",
    renders: ["ContainerCard"],
  },
  {
    file: dockerContainerCardFile,
    name: "ContainerCard",
    renders: ["FrostedCard", "ContainerCardLive"],
  },
  {
    file: dockerContainerCardFile,
    name: "ContainerCardLive",
    renders: ["ContainerCardBody"],
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
    name: "NetworkInterfaceList",
    renders: [
      "NetworkInterfaceCard",
      "NetworkInterfaceConfigurationCards",
      "NetworkInterfaceTrafficGraphs",
    ],
  },
  {
    file: "components/cards/NetworkInterfaceCard.tsx",
    name: "NetworkInterfaceCard",
    renders: ["FrostedCard", "NetworkInterfaceCardContent"],
  },
  {
    file: "routes/_authenticated/wireguard/-components/InterfaceClients.tsx",
    name: "InterfaceClients",
    renders: ["WireguardPeerCard"],
  },
  {
    file: "components/cards/WireguardPeerCard.tsx",
    name: "WireguardPeerCard",
    renders: ["FrostedCard", "WireguardPeerStatus", "WireguardPeerStats"],
  },
  {
    file: "routes/_authenticated/hardware/-components/HardwarePage.tsx",
    name: "SensorReadings",
    renders: ["SensorGroupCard"],
  },
  {
    file: "components/cards/SensorGroupCard.tsx",
    name: "SensorGroupCardShell",
    renders: ["FrostedCard", "SensorGroupCardLive"],
  },
  ...[
    ["CPUHistoryCard", "CPUHistoryLive"],
    ["MemoryHistoryCard", "MemoryHistoryLive"],
    ["DiskIOHistoryCard", "DiskIOLive"],
    ["NetworkHistoryCard", "NetworkHistoryLive"],
  ].map(([name, live]) => ({
    file: "routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx",
    name,
    renders: ["HistoryCardShell", live],
  })),
  {
    file: "components/charts/HistoryCard.tsx",
    name: "HistoryCardShell",
    renders: ["FrostedCard"],
  },
  {
    file: "routes/_authenticated/network/-components/NetworkTrafficHistoryCard.tsx",
    name: "NetworkTrafficHistoryCard",
    renders: ["HistoryCardShell", "NetworkTrafficHistoryLive"],
  },
  {
    file: "routes/_authenticated/network/-components/NetworkInterfaceLogsCard.tsx",
    name: "NetworkInterfaceLogsCard",
    renders: ["UnitLogsCard"],
  },
  {
    file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
    name: "ProtocolMountList",
    renders: ["MountCardGrid", "MountTable"],
  },
  {
    file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
    name: "MountCardGrid",
    renders: ["MountCard"],
  },
  {
    file: "components/cards/MountCard.tsx",
    name: "MountCard",
    renders: ["FrostedCard", "MountCardLiveContent"],
  },
  {
    file: "components/cards/UnitInfoPanelCard.tsx",
    name: "UnitInfoPanel",
    renders: ["FrostedCard", "UnitInfoPanelLive"],
  },
];

describe("card query ownership", () => {
  it("keeps exact direct React Query hook counts in protected components", () => {
    for (const contract of hookContracts) {
      const expected = Object.fromEntries(
        queryHooks.map((hook) => [hook, contract.hooks?.[hook] ?? 0]),
      ) as Record<QueryHook, number>;
      const component = findNamedFunction(contract);
      expect(
        hookCounts(component),
        `${contract.file}:${contract.name}`,
      ).toEqual(expected);
    }
  });

  it("keeps stable selectors attached to their direct query owners", () => {
    for (const contract of selectContracts) {
      const component = findNamedFunction(contract);
      const calls = directCalls(component, new Set<string>([contract.hook]));
      const selectPattern = new RegExp(
        `\\bselect\\s*:\\s*${contract.select}\\b`,
      );
      expect(
        calls.some((call) =>
          selectPattern.test(call.getText(component.sourceFile)),
        ),
        `${contract.file}:${contract.name} must use select: ${contract.select}`,
      ).toBe(true);

      if (contract.memoized) {
        const callbacks = directCalls(component, new Set(["useCallback"]));
        expect(
          callbacks.some((call) => {
            const declaration = call.parent;
            return (
              ts.isVariableDeclaration(declaration) &&
              ts.isIdentifier(declaration.name) &&
              declaration.name.text === contract.select &&
              declaration.initializer === call
            );
          }),
          `${contract.file}:${contract.name} must bind ${contract.select} before queryOptions`,
        ).toBe(true);
      }
    }
  });

  it("keeps WireGuard live children on the selected peer cache observer", () => {
    const file = "components/cards/WireguardPeerCard.tsx";
    const shell = findNamedFunction({ file, name: "WireguardPeerCard" });
    expect(
      directCalls(shell, new Set(["usePeer"])),
      `${file}:WireguardPeerCard`,
    ).toHaveLength(0);

    for (const name of ["WireguardPeerStatus", "WireguardPeerStats"]) {
      const component = findNamedFunction({ file, name });
      expect(
        directCalls(component, new Set(["usePeer"])),
        `${file}:${name}`,
      ).toHaveLength(1);
    }

    const usePeer = findNamedFunction({ file, name: "usePeer" });
    const calls = directCalls(usePeer, new Set(["useQuery"]));
    expect(calls, `${file}:usePeer`).toHaveLength(1);
    expect(calls[0].getText(usePeer.sourceFile)).toMatch(
      /\bselect\s*:\s*selectPeer\b/,
    );
  });

  it("keeps cache-only live observers off the polling cadence", () => {
    const cacheOnly: ComponentRef[] = [
      {
        file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
        name: "NetworkInterfaceTrafficGraphs",
      },
      {
        file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
        name: "NetworkInterfaceConfigurationCards",
      },
      {
        file: "components/cards/NetworkInterfaceCard.tsx",
        name: "NetworkInterfaceCardContent",
      },
      { file: "components/cards/WireguardPeerCard.tsx", name: "usePeer" },
      {
        file: "components/cards/SensorGroupCard.tsx",
        name: "SensorGroupCardLive",
      },
      {
        file: "components/cards/MountCard.tsx",
        name: "MountCardLiveContent",
      },
      {
        file: dockerContainerCardFile,
        name: "ContainerCardLive",
      },
    ];

    for (const contract of cacheOnly) {
      const component = findNamedFunction(contract);
      const calls = directQueryCalls(component);
      expect(calls, `${contract.file}:${contract.name}`).toHaveLength(1);
      const queryText = calls[0].getText(component.sourceFile);
      expect(queryText).toContain("refetchOnMount: false");
      expect(queryText).not.toContain("refetchInterval:");
    }
  });

  it("keeps one polling observer in each active list or standalone live owner", () => {
    const pollingOwners: ComponentRef[] = [
      {
        file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
        name: "NetworkInterfaceList",
      },
      {
        file: dockerContainerListFile,
        name: "ContainerList",
      },
      {
        file: "routes/_authenticated/wireguard/-components/InterfaceClients.tsx",
        name: "InterfaceClients",
      },
      {
        file: "routes/_authenticated/hardware/-components/HardwarePage.tsx",
        name: "SensorReadings",
      },
      ...[
        "CPUHistoryLive",
        "MemoryHistoryLive",
        "DiskIOLive",
        "NetworkHistoryLive",
      ].map((name) => ({
        file: "routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx",
        name,
      })),
      {
        file: "routes/_authenticated/network/-components/NetworkTrafficHistoryCard.tsx",
        name: "NetworkTrafficHistoryLive",
      },
      {
        file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
        name: "MountCardGrid",
      },
      {
        file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
        name: "MountTable",
      },
      {
        file: "components/cards/UnitInfoPanelCard.tsx",
        name: "UnitInfoPanelLive",
      },
    ];

    for (const contract of pollingOwners) {
      const component = findNamedFunction(contract);
      const pollingCalls = directQueryCalls(component).filter((call) =>
        call.getText(component.sourceFile).includes("refetchInterval:"),
      );
      expect(pollingCalls, `${contract.file}:${contract.name}`).toHaveLength(1);
    }
  });

  it("keeps stable identity selectors narrow", () => {
    const selectors = [
      {
        file: dockerContainerListFile,
        name: "selectCardContainers",
        includes: [
          "Created: container.Created",
          "Id: container.Id",
          "Image: container.Image",
          "Names: container.Names",
          "State: container.State",
          "Status: selectContainerSearchStatus(container)",
        ],
        excludes: ["...container", "metrics", "Status: container.Status"],
      },
      {
        file: "routes/_authenticated/network/-components/NetworkInterfaceList.tsx",
        name: "selectNetworkInterfaceIdentities",
        includes: ["veth", "name: iface.name", "type:"],
        excludes: ["...iface", "rx_speed", "tx_speed"],
      },
      {
        file: "routes/_authenticated/wireguard/-components/InterfaceClients.tsx",
        name: "selectPeerIdentities",
        includes: ["name: peer.name"],
        excludes: ["...peer", "rx_bytes", "tx_bytes", "last_handshake_unix"],
      },
      {
        file: "routes/_authenticated/hardware/-components/HardwarePage.tsx",
        name: "selectVisibleSensorGroupIdentities",
        includes: [
          "adapter: group.adapter",
          "sourceIndex",
          "visibleReadingCount",
        ],
        excludes: ["...group"],
      },
      {
        file: "routes/_authenticated/shares/-components/ProtocolMountList.tsx",
        name: "selectMountIdentities",
        includes: ["mount.mountpoint"],
        excludes: ["...mount", "usedPct", "mounted", "source"],
      },
    ];

    for (const contract of selectors) {
      const selector = findNamedFunction(contract);
      const text = selector.node.getText(selector.sourceFile);
      for (const expected of contract.includes) {
        expect(text, `${contract.file}:${contract.name}`).toContain(expected);
      }
      for (const forbidden of contract.excludes) {
        expect(text, `${contract.file}:${contract.name}`).not.toContain(
          forbidden,
        );
      }
    }
  });

  it("keeps list, card shell, and live-slot JSX ownership connected", () => {
    for (const contract of edgeContracts) {
      const component = findNamedFunction(contract);
      const rendered = renderedJsxNames(component);
      for (const expectedChild of contract.renders) {
        expect(
          rendered.has(expectedChild),
          `${contract.file}:${contract.name} must render <${expectedChild}>`,
        ).toBe(true);
      }
    }

    const dashboardCard = findNamedFunction({
      file: dashboardCardFile,
      name: "DashboardCard",
    });
    const dashboardSlots = renderedJsxValues(dashboardCard);
    for (const slot of ["headerExtras", "stats", "stats2"]) {
      expect(dashboardSlots.has(slot), `${dashboardCardFile}:${slot}`).toBe(
        true,
      );
    }

    const historyFile = "components/charts/HistoryCard.tsx";
    for (const name of ["HistoryCardShell", "HistoryCardBody"]) {
      const component = findNamedFunction({ file: historyFile, name });
      expect(
        renderedJsxValues(component).has("children"),
        `${historyFile}:${name} must render its children slot`,
      ).toBe(true);
    }
  });

  it("keeps hardware hover updates below the history live-query owners", () => {
    const hardwareFile =
      "routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx";
    const page = parseSource(
      "routes/_authenticated/hardware/-components/HardwarePage.tsx",
    );
    expect(page.sourceFile.getText()).not.toContain("historyHoverTime");

    const chartFile = "components/charts/HistoryCard.tsx";
    const synchronizedChart = findNamedFunction({
      file: chartFile,
      name: "SynchronizedHistoryAreaChart",
    });
    expect(
      directCalls(synchronizedChart, new Set(["useHistoryHover"])),
      `${chartFile}:SynchronizedHistoryAreaChart`,
    ).toHaveLength(1);
    expect(renderedJsxNames(synchronizedChart).has("HistoryAreaChart")).toBe(
      true,
    );

    for (const name of [
      "CPUHistoryLive",
      "MemoryHistoryLive",
      "DiskIOLive",
      "NetworkHistoryLive",
    ]) {
      const live = findNamedFunction({ file: hardwareFile, name });
      expect(
        directCalls(live, new Set(["useHistoryHover"])),
        `${hardwareFile}:${name}`,
      ).toHaveLength(0);
      expect(
        renderedJsxNames(live).has("SynchronizedHistoryAreaChart"),
        `${hardwareFile}:${name}`,
      ).toBe(true);
    }
  });
});
