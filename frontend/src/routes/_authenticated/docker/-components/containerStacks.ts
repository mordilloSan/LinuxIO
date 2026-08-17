import { arrayMove } from "@dnd-kit/sortable";

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
  (row as ContainerStackHeaderRow).stackHeader;

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
    // Keep members in the table model while folded. ContainerTable wraps them
    // in AppCollapse, which lets the shared disclosure motion run before they
    // unmount instead of dropping the rows synchronously.
    rows.push(...entry.containers);
  }
  return rows;
};

/**
 * Sortable id a stack band registers under, alongside the plain container ids.
 * The prefix keeps it from colliding with a container id (hex).
 */
export const STACK_DRAG_PREFIX = "stack:";

export const getStackDragId = (project: string) =>
  `${STACK_DRAG_PREFIX}${project}`;

const isStackDragId = (id: string) => id.startsWith(STACK_DRAG_PREFIX);

/**
 * Order resolver for drags involving a stack band: dragging a band moves its
 * members through the flat saved order as one block, and dropping a loose card
 * on a band lands it beside the block rather than inside it. Everything else
 * returns null and keeps the surface's default single-id move — including a
 * member dragged within its own stack.
 */
export const resolveStackDrag = (
  ids: readonly string[],
  activeId: string,
  overId: string,
  projectById: ReadonlyMap<string, string>,
): string[] | null => {
  if (!isStackDragId(activeId) && !isStackDragId(overId)) return null;

  // Rebuild the entry list the card layout derives from the flat order: loose
  // ids stand alone, multi-member projects form one block at their first
  // member.
  const memberCounts = new Map<string, number>();
  for (const id of ids) {
    const project = projectById.get(id);
    if (project) {
      memberCounts.set(project, (memberCounts.get(project) ?? 0) + 1);
    }
  }

  const entries: { key: string; ids: string[] }[] = [];
  const blocks = new Map<string, string[]>();
  for (const id of ids) {
    const project = projectById.get(id);
    if (!project || (memberCounts.get(project) ?? 0) < 2) {
      entries.push({ key: id, ids: [id] });
      continue;
    }
    const key = getStackDragId(project);
    const block = blocks.get(key);
    if (block) {
      block.push(id);
      continue;
    }
    const created = [id];
    blocks.set(key, created);
    entries.push({ key, ids: created });
  }

  const findEntryIndex = (id: string) =>
    entries.findIndex((entry) => entry.key === id || entry.ids.includes(id));
  const from = findEntryIndex(activeId);
  const to = findEntryIndex(overId);
  if (from < 0 || to < 0 || from === to) return null;
  // A member dragged onto another stack is the default flat move's business —
  // its label pins it to its own stack regardless.
  if (!isStackDragId(activeId) && entries[from].ids.length > 1) return null;

  return arrayMove(entries, from, to).flatMap((entry) => entry.ids);
};
