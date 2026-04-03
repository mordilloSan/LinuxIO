import React from "react";

import UnitLogsCard from "@/components/cards/UnitLogsCard";
import {
  DetailRow,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
  formatUsec,
} from "./UnitViews";

import type { Timer } from "@/api";
import { linuxio } from "@/api";

interface TimerCardsViewProps {
  timers: Timer[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (timer: Timer) => React.ReactNode;
}

const TimerSummaryRows: React.FC<{ timer: Timer }> = ({ timer }) => (
  <UnitStatusRows
    activeState={timer.active_state}
    subState={timer.sub_state}
    unitFileState={timer.unit_file_state}
    activeEnterTimestamp={timer.active_enter_timestamp}
    inactiveEnterTimestamp={timer.inactive_enter_timestamp}
  />
);

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

const TimerActionsWrapper: React.FC<{ timer: Timer }> = ({ timer }) => {
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(timer.name, {
    refetchInterval: 2000,
  });
  return (
    <UnitCardActions
      unitName={timer.name}
      activeState={timer.active_state}
      unitFileState={timer.unit_file_state}
      info={info}
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
    items={timers}
    expanded={expanded}
    onExpand={onExpand}
    emptyMessage="No timers found."
    renderSummaryRows={(timer) => <TimerSummaryRows timer={timer} />}
    renderSelectedRows={(timer) => <TimerSelectedRows timer={timer} />}
    renderActions={(timer) => <TimerActionsWrapper timer={timer} />}
    renderDetailPanel={renderDetailPanel}
    renderBottomPanel={(timer) => (
      <UnitLogsCard unitName={timer.name} title="Timer Logs" />
    )}
  />
);

export default TimerCardsView;
