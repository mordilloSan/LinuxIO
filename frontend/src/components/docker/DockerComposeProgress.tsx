import { Icon } from "@iconify/react";
import { useState, type CSSProperties } from "react";

import "./docker-compose-progress.css";

import AppButton from "@/components/ui/AppButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

import {
  aggregatePercent,
  type ComposeTask,
  isLayer,
  prettyId,
  shortId,
} from "./composeProgress";

interface DockerComposeProgressProps {
  tasks: ComposeTask[];
}

const isDone = (t: ComposeTask) => t.status === "Done";
const isError = (t: ComposeTask) => t.status === "Error";

// LayerRow renders a single pull layer: short id, current action, a determinate
// bar (Docker gives us `percent`), and a humanized size while downloading.
const LayerRow = ({ task }: { task: ComposeTask }) => {
  const done = isDone(task);
  const showSize = !done && !!task.total && task.total > 0;

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: "var(--app-space-4)",
        padding: "var(--app-space-2) 0",
      }}
    >
      <AppTypography
        color="text.secondary"
        noWrap
        style={{
          width: 96,
          flexShrink: 0,
          fontFamily: "var(--app-font-mono)",
        }}
        title={task.id}
        tooltipOnlyWhenTruncated={false}
        variant="caption"
      >
        {shortId(task.id)}
      </AppTypography>
      <AppTypography
        noWrap
        style={{
          width: 150,
          flexShrink: 0,
        }}
        title={task.text}
        variant="body2"
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
          fontVariantNumeric: "tabular-nums",
        }}
        variant="caption"
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
  first: boolean; // no leading gap on the first group
  hasLayers: boolean;
  controlsId: string;
  onToggle: () => void;
}

// GroupHeader renders a collapsible Image/Container/… section header. When
// collapsed it shows a compact summary bar so the section state stays visible;
// when expanded the per-layer rows below carry the detail instead.
const GroupHeader = ({
  task,
  percent,
  expanded,
  first,
  hasLayers,
  controlsId,
  onToggle,
}: GroupHeaderProps) => {
  const done = isDone(task);

  const headerStyle: CSSProperties = {
    alignItems: "center",
    background: "none",
    border: 0,
    cursor: hasLayers ? "pointer" : "default",
    display: "flex",
    // AppButton centres its content; without the summary bar's flex:1 there is
    // nothing left to push this row against its left edge.
    justifyContent: "flex-start",
    gap: "var(--app-space-4)",
    marginBottom: expanded && hasLayers ? "var(--app-space-6)" : 0,
    marginTop: first ? 0 : "var(--app-space-8)",
    padding: 0,
    textAlign: "left",
    userSelect: "none",
    width: "100%",
  };

  const content = (
    <>
      {hasLayers ? (
        <Icon
          color="var(--app-palette-text-secondary)"
          height={18}
          icon={expanded ? "mdi:chevron-down" : "mdi:chevron-right"}
          width={18}
        />
      ) : (
        <span style={{ width: 18, flexShrink: 0 }} />
      )}
      {done ? (
        <Icon
          color="var(--app-palette-success-main)"
          height={16}
          icon="mdi:check-circle"
          width={16}
        />
      ) : isError(task) ? (
        <Icon
          color="var(--app-palette-error-main)"
          height={16}
          icon="mdi:alert-circle"
          width={16}
        />
      ) : (
        <Icon
          className="compose-progress__spin"
          color="var(--app-palette-text-secondary)"
          height={16}
          icon="mdi:loading"
          width={16}
        />
      )}
      <AppTypography fontWeight={600} variant="body2">
        {prettyId(task.id)}
      </AppTypography>
      <AppTypography color="text.secondary" variant="body2">
        {task.text}
      </AppTypography>

      {/* A finished group states its outcome in words ("Pulled"), so the
          summary bar retires with the work it was tracking. */}
      {!expanded && !done && percent !== null && (
        <>
          <div
            style={{
              flex: 1,
              minWidth: 80,
              marginLeft: "var(--app-space-4)",
            }}
          >
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
              fontVariantNumeric: "tabular-nums",
            }}
            variant="caption"
          >
            {percent}%
          </AppTypography>
        </>
      )}
    </>
  );

  if (!hasLayers) {
    return <div style={headerStyle}>{content}</div>;
  }

  return (
    <AppButton
      aria-controls={controlsId}
      aria-expanded={expanded}
      aria-label={`${prettyId(task.id)} details`}
      color="inherit"
      onClick={onToggle}
      style={headerStyle}
    >
      {content}
    </AppButton>
  );
};

const DockerComposeProgress = ({ tasks }: DockerComposeProgressProps) => {
  // Per-group user override of expansion. Absent => collapsed by default; the
  // user expands a section on demand.
  const [collapsedOverride, setCollapsedOverride] = useState<
    Map<string, boolean>
  >(new Map());

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
    <div style={{ padding: "var(--app-space-8)" }}>
      {groups.map((g, index) => {
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
              controlsId={`compose-progress-${encodeURIComponent(g.id)}`}
              expanded={expanded}
              first={index === 0}
              hasLayers={layers.length > 0}
              onToggle={() => toggle(g.id, expanded)}
              percent={groupPercent}
              task={g}
            />
            {layers.length > 0 && (
              <div
                aria-hidden={!expanded}
                id={`compose-progress-${encodeURIComponent(g.id)}`}
                style={{ display: expanded ? undefined : "none" }}
              >
                {layers.map((layer) => (
                  <LayerRow key={layer.id} task={layer} />
                ))}
              </div>
            )}
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
