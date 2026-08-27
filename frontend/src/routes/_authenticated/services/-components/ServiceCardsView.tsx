import { useSuspenseQuery } from "@tanstack/react-query";
import { memo, type ReactNode } from "react";

import type { Service } from "@/api";
import { linuxio } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import AppTypography from "@/components/ui/AppTypography";
import { getServiceStatusColor } from "@/constants/statusColors";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";

import { formatBytes } from "./unitFormatters";
import {
  DetailRow,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
} from "./UnitViews";

interface ServiceCardsViewProps {
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (service: Service) => ReactNode;
  services: Service[];
  surface: ReorderableSurface<Service>;
}

const ServiceStatusRows = memo<{ service: Service }>(({ service }) => (
  <UnitStatusRows
    activeEnterTimestamp={service.active_enter_timestamp}
    activeLabel="Running"
    activeState={service.active_state}
    inactiveEnterTimestamp={service.inactive_enter_timestamp}
    subState={service.sub_state}
    unitFileState={service.unit_file_state}
  />
));
ServiceStatusRows.displayName = "ServiceStatusRows";

const ServiceInfoRows = ({ service }: { service: Service }) => {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: service.name }),
    refetchInterval: 2000,
  });
  const mainPid = info?.MainPID ?? 0;
  const memory = formatBytes(info?.MemoryCurrent);
  const statusColor = getServiceStatusColor(service.active_state);

  return (
    <>
      <DetailRow label="Active">
        <AppTypography
          color={statusColor}
          component="span"
          fontWeight={500}
          variant="caption"
        >
          {service.active_state}
        </AppTypography>
      </DetailRow>
      <DetailRow label="Load">
        <AppTypography
          color={
            service.load_state === "loaded" ? "text.primary" : "text.secondary"
          }
          component="span"
          fontWeight={500}
          variant="caption"
        >
          {service.load_state}
        </AppTypography>
      </DetailRow>
      {mainPid > 0 && (
        <DetailRow label="PID">
          <AppTypography component="span" fontWeight={500} variant="caption">
            {mainPid}
          </AppTypography>
        </DetailRow>
      )}
      {memory !== "—" && (
        <DetailRow label="Memory">
          <AppTypography component="span" fontWeight={500} variant="caption">
            {memory}
          </AppTypography>
        </DetailRow>
      )}
    </>
  );
};

const ServiceActionsWrapper = ({ service }: { service: Service }) => {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: service.name }),
    refetchInterval: 2000,
  });
  return (
    <UnitCardActions
      activeState={service.active_state}
      info={info}
      unitFileState={service.unit_file_state}
      unitName={service.name}
    />
  );
};

const ServiceCardsView = ({
  services,
  expanded,
  onExpand,
  renderDetailPanel,
  surface,
}: ServiceCardsViewProps) => (
  <UnitCardsView
    emptyMessage="No services found."
    expanded={expanded}
    items={services}
    surface={surface}
    onExpand={onExpand}
    renderActions={(service) => <ServiceActionsWrapper service={service} />}
    renderBottomPanel={(service) => (
      <UnitLogsCard title="Service Logs" unitName={service.name} />
    )}
    renderDetailPanel={renderDetailPanel}
    renderSelectedRows={(service) => <ServiceInfoRows service={service} />}
    renderSummaryRows={(service) => <ServiceStatusRows service={service} />}
  />
);

export default ServiceCardsView;
