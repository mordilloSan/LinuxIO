import type { ContainerInfo } from "@/api";

/** Docker Compose stamps every container it creates with its project name. */
export const COMPOSE_PROJECT_LABEL = "com.docker.compose.project";

export const getComposeProject = (
  container: ContainerInfo,
): string | undefined => {
  const project = container.Labels?.[COMPOSE_PROJECT_LABEL]?.trim();
  return project ? project : undefined;
};

export type ContainerStackEntry =
  | { type: "container"; container: ContainerInfo }
  | { type: "stack"; project: string; containers: ContainerInfo[] };

/**
 * Splits a container list into loose containers and compose stacks, in place: a
 * stack sits where its first member sits, and members keep their relative
 * order, so the saved manual order still decides the layout. Only projects with
 * two or more containers present become stacks — grouping a single container
 * would add chrome without organizing anything.
 */
export const groupContainersByStack = (
  containers: readonly ContainerInfo[],
): ContainerStackEntry[] => {
  const memberCounts = new Map<string, number>();
  for (const container of containers) {
    const project = getComposeProject(container);
    if (project) {
      memberCounts.set(project, (memberCounts.get(project) ?? 0) + 1);
    }
  }

  const entries: ContainerStackEntry[] = [];
  const stacks = new Map<string, ContainerInfo[]>();
  for (const container of containers) {
    const project = getComposeProject(container);
    if (!project || (memberCounts.get(project) ?? 0) < 2) {
      entries.push({ type: "container", container });
      continue;
    }
    const members = stacks.get(project);
    if (members) {
      members.push(container);
      continue;
    }
    const created = [container];
    stacks.set(project, created);
    entries.push({ type: "stack", project, containers: created });
  }
  return entries;
};

export interface ContainerStackSummary {
  running: number;
  total: number;
  unhealthy: boolean;
  updateAvailable: boolean;
}

export const summarizeStack = (
  containers: readonly ContainerInfo[],
): ContainerStackSummary => ({
  running: containers.filter((container) => container.State === "running")
    .length,
  total: containers.length,
  unhealthy: containers.some((container) =>
    container.Status.toLowerCase().includes("unhealthy"),
  ),
  updateAvailable: containers.some(
    (container) => container.updateAvailable === true,
  ),
});

/** Maps a stack summary onto the display states `getContainerStatusColor` knows. */
export const getStackDisplayState = (summary: ContainerStackSummary) => {
  if (summary.unhealthy) return "Unhealthy";
  if (summary.running === 0) return "Stopped";
  if (summary.running < summary.total) return "Partially running";
  return "Running";
};

export const formatStackSummary = (summary: ContainerStackSummary) =>
  `${summary.total} containers · ${summary.running} running`;

/**
 * The synthetic row the container table interleaves above a stack's members.
 * `renderRow` swaps the whole row for a header, so none of the container
 * columns ever render against it.
 */
export interface ContainerStackHeaderRow {
  stackHeader: true;
  project: string;
  collapsed: boolean;
  containers: ContainerInfo[];
}

export type ContainerTableRow = ContainerInfo | ContainerStackHeaderRow;

export const isStackHeaderRow = (
  row: unknown,
): row is ContainerStackHeaderRow =>
  typeof row === "object" &&
  row !== null &&
  (row as ContainerStackHeaderRow).stackHeader === true;

export const buildContainerTableRows = (
  containers: readonly ContainerInfo[],
  collapsedStackIds: ReadonlySet<string>,
): ContainerTableRow[] => {
  const rows: ContainerTableRow[] = [];
  for (const entry of groupContainersByStack(containers)) {
    if (entry.type === "container") {
      rows.push(entry.container);
      continue;
    }
    const collapsed = collapsedStackIds.has(entry.project);
    rows.push({
      collapsed,
      containers: entry.containers,
      project: entry.project,
      stackHeader: true,
    });
    if (!collapsed) rows.push(...entry.containers);
  }
  return rows;
};
