import { Icon } from "@iconify/react";
import React, { useState } from "react";

import {
  aggregatePercent,
  type ComposeTask,
  isLayer,
  prettyId,
  shortId,
} from "./composeProgress";
import "./docker-compose-progress.css";

import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

interface DockerComposeProgressProps {
  tasks: ComposeTask[];
}

const isDone = (t: ComposeTask) => t.status === "Done";
const isError = (t: ComposeTask) => t.status === "Error";

// LayerRow renders a single pull layer: short id, current action, a determinate
// bar (Docker gives us `percent`), and a humanized size while downloading.
const LayerRow: React.FC<{ task: ComposeTask }> = ({ task }) => {
  const theme = useAppTheme();
  const done = isDone(task);
  const showSize = !done && !!task.total && task.total > 0;

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(1),
        padding: `${theme.spacing(0.25)} 0`,
      }}
    >
      <AppTypography
        color="text.secondary"
        noWrap
        style={{
          width: 96,
          flexShrink: 0,
          fontFamily: "monospace",
          fontSize: "0.75rem",
        }}
        title={task.id}
        tooltipOnlyWhenTruncated={false}
      >
        {shortId(task.id)}
      </AppTypography>
      <AppTypography
        noWrap
        style={{
          width: 150,
          flexShrink: 0,
          fontSize: "0.8rem",
        }}
        title={task.text}
      >
        {task.text}
      </AppTypography>
      <div style={{ flex: 1, minWidth: 80 }}>
        <AppLinearProgress
          color={isError(task) ? "error" : done ? "success" : "primary"}
          value={task.pct}
          variant="determinate"
        />
      </div>
      <AppTypography
        color="text.secondary"
        noWrap
        style={{
          width: 130,
          flexShrink: 0,
          textAlign: "right",
          fontSize: "0.75rem",
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {done
          ? "✓"
          : showSize
            ? `${formatFileSize(task.current ?? 0, 1, "")} / ${formatFileSize(task.total, 1, "")}`
            : `${task.pct}%`}
      </AppTypography>
    </div>
  );
};

interface GroupHeaderProps {
  task: ComposeTask;
  percent: number | null; // group completion, or null when it has no layers
  expanded: boolean;
  hasLayers: boolean;
  onToggle: () => void;
}

// GroupHeader renders a collapsible Image/Container/… section header. When
// collapsed it shows a compact summary bar so the section state stays visible;
// when expanded the per-layer rows below carry the detail instead.
const GroupHeader: React.FC<GroupHeaderProps> = ({
  task,
  percent,
  expanded,
  hasLayers,
  onToggle,
}) => {
  const theme = useAppTheme();
  const done = isDone(task);

  return (
    <div
      onClick={hasLayers ? onToggle : undefined}
      style={{
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(1),
        marginTop: theme.spacing(2.5),
        marginBottom: expanded && hasLayers ? theme.spacing(1.5) : 0,
        cursor: hasLayers ? "pointer" : "default",
        userSelect: "none",
      }}
    >
      {hasLayers ? (
        <Icon
          color={theme.palette.text.secondary}
          height={18}
          icon={expanded ? "mdi:chevron-down" : "mdi:chevron-right"}
          width={18}
        />
      ) : (
        <span style={{ width: 18, flexShrink: 0 }} />
      )}
      {done ? (
        <Icon
          color={theme.palette.success.main}
          height={16}
          icon="mdi:check-circle"
          width={16}
        />
      ) : isError(task) ? (
        <Icon
          color={theme.palette.error.main}
          height={16}
          icon="mdi:alert-circle"
          width={16}
        />
      ) : (
        <Icon
          className="compose-progress__spin"
          color={theme.palette.text.secondary}
          height={16}
          icon="mdi:loading"
          width={16}
        />
      )}
      <AppTypography style={{ fontWeight: 600, fontSize: "0.85rem" }}>
        {prettyId(task.id)}
      </AppTypography>
      <AppTypography color="text.secondary" style={{ fontSize: "0.8rem" }}>
        {task.text}
      </AppTypography>

      {/* Compact summary bar for collapsed sections. */}
      {!expanded && percent !== null && (
        <>
          <div style={{ flex: 1, minWidth: 80, marginLeft: theme.spacing(1) }}>
            <AppLinearProgress
              color={percent >= 100 ? "success" : "primary"}
              value={percent}
              variant="determinate"
            />
          </div>
          <AppTypography
            color="text.secondary"
            style={{
              width: 40,
              flexShrink: 0,
              textAlign: "right",
              fontSize: "0.75rem",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {percent}%
          </AppTypography>
        </>
      )}
    </div>
  );
};

const DockerComposeProgress: React.FC<DockerComposeProgressProps> = ({
  tasks,
}) => {
  const theme = useAppTheme();
  // Per-group user override of expansion. Absent => collapsed by default; the
  // user expands a section on demand.
  const [collapsedOverride, setCollapsedOverride] = useState<
    Map<string, boolean>
  >(new Map());
  const overall = aggregatePercent(tasks);

  // Groups (Image/Container/…) keep Map insertion order; layers are nested
  // under their parent image. Layers whose parent hasn't appeared yet are
  // grouped by their parent_id string so nothing is dropped.
  const groups = tasks.filter((t) => !isLayer(t));
  const layersByParent = new Map<string, ComposeTask[]>();
  for (const t of tasks) {
    if (!isLayer(t) || !t.parent_id) continue;
    const list = layersByParent.get(t.parent_id) ?? [];
    list.push(t);
    layersByParent.set(t.parent_id, list);
  }

  const groupIds = new Set(groups.map((g) => g.id));
  const orphanParents = [...layersByParent.keys()].filter(
    (pid) => !groupIds.has(pid),
  );

  const toggle = (id: string, currentlyExpanded: boolean) =>
    setCollapsedOverride((prev) => {
      const next = new Map(prev);
      next.set(id, currentlyExpanded); // collapse if it was expanded, and vice-versa
      return next;
    });

  return (
    <div style={{ padding: theme.spacing(2) }}>
      {overall !== null && (
        <div style={{ marginBottom: theme.spacing(2) }}>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              marginBottom: theme.spacing(0.5),
            }}
          >
            <AppTypography style={{ fontWeight: 600 }}>Overall</AppTypography>
            <AppTypography
              color="text.secondary"
              style={{ fontVariantNumeric: "tabular-nums" }}
            >
              {overall}%
            </AppTypography>
          </div>
          <AppLinearProgress
            color={overall >= 100 ? "success" : "primary"}
            value={overall}
            variant="determinate"
          />
        </div>
      )}

      {groups.map((g) => {
        const layers = layersByParent.get(g.id) ?? [];
        const groupPercent =
          layers.length > 0 ? aggregatePercent(layers) : null;
        const override = collapsedOverride.get(g.id);
        // Collapsed by default; the per-group header bar shows progress, and the
        // user expands a section on demand to see its per-layer rows.
        const expanded = override !== undefined ? !override : false;
        return (
          <div key={g.id}>
            <GroupHeader
              expanded={expanded}
              hasLayers={layers.length > 0}
              onToggle={() => toggle(g.id, expanded)}
              percent={groupPercent}
              task={g}
            />
            {expanded &&
              layers.map((layer) => <LayerRow key={layer.id} task={layer} />)}
          </div>
        );
      })}

      {orphanParents.map((pid) => (
        <div key={pid}>
          {(layersByParent.get(pid) ?? []).map((layer) => (
            <LayerRow key={layer.id} task={layer} />
          ))}
        </div>
      ))}
    </div>
  );
};

export default DockerComposeProgress;
