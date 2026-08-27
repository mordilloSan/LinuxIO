import { useSuspenseQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import type { Timer } from "@/api";
import { linuxio } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import AppTypography from "@/components/ui/AppTypography";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";

import { formatUsec } from "./unitFormatters";
import {
  DetailRow,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
} from "./UnitViews";

interface TimerCardsViewProps {
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (timer: Timer) => ReactNode;
  timers: Timer[];
  surface: ReorderableSurface<Timer>;
}

const TimerSummaryRows = ({ timer }: { timer: Timer }) => (
  <UnitStatusRows
    activeEnterTimestamp={timer.active_enter_timestamp}
    activeState={timer.active_state}
    inactiveEnterTimestamp={timer.inactive_enter_timestamp}
    subState={timer.sub_state}
    unitFileState={timer.unit_file_state}
  />
);

const TimerSelectedRows = ({ timer }: { timer: Timer }) => {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: timer.name }),
    refetchInterval: 2000,
  });

  return (
    <>
      <DetailRow label="Load">
        <AppTypography component="span" fontWeight={500} variant="caption">
          {timer.load_state}
        </AppTypography>
      </DetailRow>
      <DetailRow label="Unit">
        <AppTypography component="span" fontWeight={500} variant="caption">
          {info?.Unit ?? timer.unit ?? "—"}
        </AppTypography>
      </DetailRow>
      <DetailRow label="Next">
        <AppTypography component="span" fontWeight={500} variant="caption">
          {formatUsec(info?.NextElapseUSec ?? timer.next_elapse_usec)}
        </AppTypography>
      </DetailRow>
      <DetailRow label="Last">
        <AppTypography component="span" fontWeight={500} variant="caption">
          {formatUsec(info?.LastTriggerUSec ?? timer.last_trigger_usec)}
        </AppTypography>
      </DetailRow>
    </>
  );
};

const TimerActionsWrapper = ({ timer }: { timer: Timer }) => {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: timer.name }),
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

const TimerCardsView = ({
  timers,
  expanded,
  onExpand,
  renderDetailPanel,
  surface,
}: TimerCardsViewProps) => (
  <UnitCardsView
    emptyMessage="No timers found."
    expanded={expanded}
    items={timers}
    surface={surface}
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
