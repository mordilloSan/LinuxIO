import { useCallback } from "react";

import type { Service } from "@/api";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { useVirtualReorderableTableDnd } from "@/hooks/useReorderableTableDnd";

import UnitStatusDot from "./UnitStatusDot";
import { UnitTableView } from "./UnitViews";

interface ServiceTableViewProps {
  onSelect?: (name: string | null) => void;
  services: Service[];
  surface: ReorderableSurface<Service>;
}

const desktopColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "120px",
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const, width: "200px" },
  {
    field: "load_state",
    headerName: "Load State",
    align: "left" as const,
    width: "120px",
  },
  {
    field: "sub_state",
    headerName: "Sub State",
    align: "left" as const,
    width: "120px",
  },
  { field: "description", headerName: "Description", align: "left" as const },
];

const mobileColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "110px",
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const },
];

const getServiceRowKey = (service: Service) => service.name;

const renderServiceMainRow = (service: Service, isMobile: boolean) => [
  <>
    <UnitStatusDot activeState={service.active_state} />
    {service.active_state}
  </>,
  service.name,
  ...(isMobile
    ? []
    : [service.load_state, service.sub_state, service.description || "-"]),
];

const ServiceTableView = ({
  surface,
  services,
  onSelect,
}: ServiceTableViewProps) => {
  const handleSelect = useCallback(
    (key: string | number | null) =>
      onSelect?.(typeof key === "string" ? key : null),
    [onSelect],
  );

  const dnd = useVirtualReorderableTableDnd<Service, Service>({ surface });

  return (
    <UnitTableView
      dnd={dnd}
      data={services}
      desktopColumns={desktopColumns}
      emptyMessage="No services found."
      getRowKey={getServiceRowKey}
      mobileColumns={mobileColumns}
      onSelect={handleSelect}
      renderMainRow={renderServiceMainRow}
    />
  );
};

export default ServiceTableView;
