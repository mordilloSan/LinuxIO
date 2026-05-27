import React from "react";

import type { Timer } from "@/api";

import { linuxio } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";

import {
  DetailRow,
  formatUsec,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
} from "./UnitViews";

interface TimerCardsViewProps {
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (timer: Timer) => React.ReactNode;
  timers: Timer[];
}

const TimerSummaryRows: React.FC<{ timer: Timer }> = ({ timer }) => (
  <UnitStatusRows
    activeEnterTimestamp={timer.active_enter_timestamp}
    activeState={timer.active_state}
    inactiveEnterTimestamp={timer.inactive_enter_timestamp}
    subState={timer.sub_state}
    unitFileState={timer.unit_file_state}
  />
);

const TimerSelectedRows: React.FC<{ timer: Timer }> = ({ timer }) => {
  const { data: info } = linuxio.systemd.get_unit_info.useQuery(timer.name, {
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

const TimerActionsWrapper: React.FC<{ timer: Timer }> = ({ timer }) => {
  const { data: info } = linuxio.systemd.get_unit_info.useQuery(timer.name, {
    refetchInterval: 2000,
  });
  return (
    <UnitCardActions
      activeState={timer.active_state}
      info={info}
      unitFileState={timer.unit_file_state}
      unitName={timer.name}
    />
  );
};

const TimerCardsView: React.FC<TimerCardsViewProps> = ({
  timers,
  expanded,
  onExpand,
  renderDetailPanel,
}) => (
  <UnitCardsView
    emptyMessage="No timers found."
    expanded={expanded}
    items={timers}
    onExpand={onExpand}
    renderActions={(timer) => <TimerActionsWrapper timer={timer} />}
    renderBottomPanel={(timer) => (
      <UnitLogsCard title="Timer Logs" unitName={timer.name} />
    )}
    renderDetailPanel={renderDetailPanel}
    renderSelectedRows={(timer) => <TimerSelectedRows timer={timer} />}
    renderSummaryRows={(timer) => <TimerSummaryRows timer={timer} />}
  />
);

export default TimerCardsView;
