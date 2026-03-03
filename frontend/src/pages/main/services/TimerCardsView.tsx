import React from "react";

import {
  AutoStartRow,
  DetailRow,
  UnitCardsView,
  formatUsec,
} from "./UnitViews";

import type { Timer } from "@/api";
import { linuxio } from "@/api";
import { getServiceStatusColor } from "@/constants/statusColors";

interface TimerCardsViewProps {
  timers: Timer[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (timer: Timer) => React.ReactNode;
}

const TimerSummaryRows: React.FC<{ timer: Timer }> = ({ timer }) => {
  const statusColor = getServiceStatusColor(timer.active_state);

  return (
    <>
      <DetailRow label="Status" noBorder>
        <span
          style={{
            fontSize: "0.85rem",
            fontWeight: 600,
            color: statusColor,
          }}
        >
          {timer.active_state}
          {timer.sub_state && timer.sub_state !== timer.active_state && (
            <span
              style={{
                color: "var(--mui-palette-text-secondary)",
                marginLeft: 8,
                fontWeight: 400,
              }}
            >
              ({timer.sub_state})
            </span>
          )}
        </span>
      </DetailRow>
      <AutoStartRow unitFileState={timer.unit_file_state} />
    </>
  );
};

const TimerSelectedRows: React.FC<{ timer: Timer }> = ({ timer }) => {
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(timer.name, {
    refetchInterval: 2000,
  });

  return (
    <>
      <DetailRow label="Load">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {timer.load_state}
        </span>
      </DetailRow>
      <DetailRow label="Unit">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {String(info?.Unit ?? timer.unit ?? "—")}
        </span>
      </DetailRow>
      <DetailRow label="Next">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {formatUsec(info?.NextElapseUSec ?? timer.next_elapse_usec)}
        </span>
      </DetailRow>
      <DetailRow label="Last">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {formatUsec(info?.LastTriggerUSec ?? timer.last_trigger_usec)}
        </span>
      </DetailRow>
    </>
  );
};

const TimerCardsView: React.FC<TimerCardsViewProps> = ({
  timers,
  expanded,
  onExpand,
  renderDetailPanel,
}) => (
  <UnitCardsView
    items={timers}
    expanded={expanded}
    onExpand={onExpand}
    emptyMessage="No timers found."
    renderSummaryRows={(timer) => <TimerSummaryRows timer={timer} />}
    renderSelectedRows={(timer) => <TimerSelectedRows timer={timer} />}
    renderDetailPanel={renderDetailPanel}
  />
);

export default TimerCardsView;
