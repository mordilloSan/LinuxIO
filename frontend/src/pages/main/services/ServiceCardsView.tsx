import React from "react";

import UnitLogsCard from "./UnitLogsCard";
import {
  DetailRow,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
  formatBytes,
} from "./UnitViews";

import type { Service, UnitInfo } from "@/api";
import { linuxio } from "@/api";
import { getServiceStatusColor } from "@/constants/statusColors";

interface ServiceCardsViewProps {
  services: Service[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (service: Service) => React.ReactNode;
}

const ServiceStatusRows = React.memo<{ service: Service }>(({ service }) => (
  <UnitStatusRows
    activeState={service.active_state}
    subState={service.sub_state}
    unitFileState={service.unit_file_state}
    activeEnterTimestamp={service.active_enter_timestamp}
    inactiveEnterTimestamp={service.inactive_enter_timestamp}
    activeLabel="Running"
  />
));
ServiceStatusRows.displayName = "ServiceStatusRows";

const ServiceInfoRows: React.FC<{ service: Service }> = ({ service }) => {
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(service.name, {
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
                ? "var(--mui-palette-text-primary)"
                : "var(--mui-palette-text-secondary)",
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
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(service.name, {
    refetchInterval: 2000,
  });
  return (
    <UnitCardActions
      unitName={service.name}
      activeState={service.active_state}
      unitFileState={service.unit_file_state}
      info={info}
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
    items={services}
    expanded={expanded}
    onExpand={onExpand}
    emptyMessage="No services found."
    renderSummaryRows={(service) => <ServiceStatusRows service={service} />}
    renderSelectedRows={(service) => <ServiceInfoRows service={service} />}
    renderActions={(service) => <ServiceActionsWrapper service={service} />}
    renderDetailPanel={renderDetailPanel}
    renderBottomPanel={(service) => (
      <UnitLogsCard unitName={service.name} title="Service Logs" />
    )}
  />
);

export default ServiceCardsView;
