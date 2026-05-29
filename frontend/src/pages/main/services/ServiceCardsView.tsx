import React from "react";

import {
  DetailRow,
  formatBytes,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
} from "./UnitViews";

import type { Service } from "@/api";
import { linuxio } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import { getServiceStatusColor } from "@/constants/statusColors";

interface ServiceCardsViewProps {
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (service: Service) => React.ReactNode;
  services: Service[];
}

const ServiceStatusRows = React.memo<{ service: Service }>(({ service }) => (
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

const ServiceInfoRows: React.FC<{ service: Service }> = ({ service }) => {
  const { data: info } = linuxio.systemd.get_unit_info.useQuery(service.name, {
    refetchInterval: 2000,
  });
  const mainPid = Number(info?.MainPID ?? 0);
  const memory = formatBytes(info?.MemoryCurrent);
  const statusColor = getServiceStatusColor(service.active_state);

  return (
    <>
      <DetailRow label="Active">
        <span
          style={{
            fontSize: "0.75rem",
            fontWeight: 500,
            color: statusColor,
          }}
        >
          {service.active_state}
        </span>
      </DetailRow>
      <DetailRow label="Load">
        <span
          style={{
            fontSize: "0.75rem",
            fontWeight: 500,
            color:
              service.load_state === "loaded"
                ? "var(--app-palette-text-primary)"
                : "var(--app-palette-text-secondary)",
          }}
        >
          {service.load_state}
        </span>
      </DetailRow>
      {mainPid > 0 && (
        <DetailRow label="PID">
          <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
            {mainPid}
          </span>
        </DetailRow>
      )}
      {memory !== "—" && (
        <DetailRow label="Memory">
          <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>{memory}</span>
        </DetailRow>
      )}
    </>
  );
};

const ServiceActionsWrapper: React.FC<{ service: Service }> = ({ service }) => {
  const { data: info } = linuxio.systemd.get_unit_info.useQuery(service.name, {
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

const ServiceCardsView: React.FC<ServiceCardsViewProps> = ({
  services,
  expanded,
  onExpand,
  renderDetailPanel,
}) => (
  <UnitCardsView
    emptyMessage="No services found."
    expanded={expanded}
    items={services}
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
