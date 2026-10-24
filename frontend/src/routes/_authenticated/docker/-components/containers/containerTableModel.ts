import type { ContainerInfo } from "@/api";
import type { AppVirtualTableCellRenderKey } from "@/components/tables/AppVirtualTable.types";
import { getContainerMounts, getDedupedPorts } from "@/utils/dockerContainer";
import { formatFileSize, formatRelativeAge } from "@/utils/formatters";

import {
  getComposeProject,
  isStackHeaderRow,
  type ContainerTableRow,
} from "./containerStacks";

const getImageVersion = (image: string) => {
  const noDigest = image.split("@")[0];
  const parts = noDigest.split(":");
  if (parts.length < 2) return "-";
  return parts[parts.length - 1] || "-";
};

// Docker merges the image's labels into the container's, so a well-behaved
// image tells us what "latest" currently resolves to.
const VERSION_LABEL_KEYS = [
  "org.opencontainers.image.version",
  "org.label-schema.version",
  "version",
];

const getLabelVersion = (labels?: Record<string, string>) => {
  if (!labels) return undefined;
  for (const key of VERSION_LABEL_KEYS) {
    const value = labels[key]?.trim();
    if (value) return value;
  }
  return undefined;
};

// A floating tag says nothing about what is actually running, so pair it with
// the labelled version when the image publishes one.
export const getVersionDisplay = (container: ContainerInfo) => {
  const tag = getImageVersion(container.Image);
  const labelVersion = getLabelVersion(container.Labels);

  if (!labelVersion || labelVersion === tag) return tag;
  if (tag === "-") return labelVersion;
  if (tag === "latest") return `${tag} (${labelVersion})`;
  return tag;
};

type ContainerUpdateStatusInput = Pick<
  ContainerInfo,
  | "updateAvailable"
  | "updateCheckedAt"
  | "updateCheckReason"
  | "updateCheckState"
  | "updateError"
>;

// `label` is the visible second line of the Version cell; `detail` is the
// tooltip's first line — how old the verdict is, which the label deliberately
// does not claim, since a scan is only as current as when it last ran.
export const getUpdateStatus = ({
  updateAvailable,
  updateCheckedAt,
  updateCheckReason,
  updateCheckState,
  updateError,
}: ContainerUpdateStatusInput) => {
  const detail = updateCheckedAt
    ? `Scanned ${formatRelativeAge(updateCheckedAt)}`
    : "Never scanned";

  if (updateCheckState === "uncheckable") {
    return {
      color: "secondary" as const,
      detail: updateCheckReason || "This image has no repository digest",
      label: "Cannot check",
    };
  }
  if (updateCheckState === "error" || updateError) {
    return {
      color: "error" as const,
      detail: updateError,
      label: "Scan failed",
    };
  }
  if (updateAvailable === true) {
    return {
      color: "warning" as const,
      detail: updateCheckReason || detail,
      label: "Update available",
    };
  }
  if (updateAvailable === false || updateCheckedAt) {
    return { color: "success" as const, detail, label: "Up to date" };
  }
  return { color: "inherit" as const, detail, label: "Not scanned" };
};

// A collapsed ports/volumes cell renders two entries plus a "+N more" caption,
// which is exactly the height an untruncated three-entry list takes. Collapsing
// only buys space from the fourth entry on, so three entries stay fully visible
// and the row offers no expander.
const COLLAPSED_ENTRIES = 2;

export const isCollapsible = (total: number) => total > COLLAPSED_ENTRIES + 1;

export const getVisibleEntries = (total: number) =>
  isCollapsible(total) ? COLLAPSED_ENTRIES : total;

const getContainerTableSignature = (container: ContainerInfo) => {
  const networks = Object.entries(container.NetworkSettings?.Networks ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(
      ([networkName, endpoint]) =>
        `${networkName}:${endpoint.IPAddress || "-"}`,
    )
    .join("|");
  const ports = getDedupedPorts(container)
    .map((port) => `${port.PrivatePort}:${port.PublicPort ?? "-"}:${port.Type}`)
    .join("|");
  const mounts = getContainerMounts(container)
    .map((mount) => `${mount.Type}:${mount.Destination}:${mount.Source}`)
    .join("|");

  return [
    container.Id,
    container.Names?.join("|") ?? "",
    container.Image,
    container.State,
    container.Status,
    container.Created,
    // Grouping input: a changed project label regroups the rows.
    getComposeProject(container) ?? "",
    container.icon ?? "",
    container.url ?? "",
    container.updateAvailable === undefined
      ? ""
      : String(container.updateAvailable),
    container.updateCheckedAt ?? "",
    container.updateCheckReason ?? "",
    container.updateCheckState ?? "",
    container.updateError ?? "",
    container.metrics?.status ?? "",
    container.metrics?.cpu_percent?.toFixed(1) ?? "",
    container.metrics?.memory_usage_bytes === undefined
      ? ""
      : formatFileSize(container.metrics.memory_usage_bytes),
    networks,
    ports,
    mounts,
  ].join("\u001f");
};

export const areStringSetsEqual = (
  left: ReadonlySet<string>,
  right: ReadonlySet<string>,
) => {
  if (left === right) return true;
  if (left.size !== right.size) return false;
  for (const value of left) {
    if (!right.has(value)) return false;
  }
  return true;
};

export const areContainerArraysEquivalent = (
  left: ContainerInfo[],
  right: ContainerInfo[],
) => {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) {
    if (
      getContainerTableSignature(left[index]) !==
      getContainerTableSignature(right[index])
    ) {
      return false;
    }
  }
  return true;
};

// Cells only ever mount against container rows — `renderRow` swaps the whole
// row out for stack headers — so the cast is safe there. The render-key metas
// do run against header rows while the cell models are built, so they go
// through the guard below, which hands headers a stable key instead.
export const asContainer = (row: unknown) => row as ContainerInfo;

export const containerCellRenderKey =
  (getKey: (container: ContainerInfo) => AppVirtualTableCellRenderKey) =>
  (row: unknown): AppVirtualTableCellRenderKey =>
    isStackHeaderRow(row) ? [row.project] : getKey(asContainer(row));

export const getContainerTableRowId = (row: ContainerTableRow) =>
  isStackHeaderRow(row) ? `stack:${row.project}` : row.Id;
