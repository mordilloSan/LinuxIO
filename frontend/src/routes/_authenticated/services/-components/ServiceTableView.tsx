import { useCallback } from "react";

import type { Service } from "@/api";
import { AppTableCell } from "@/components/ui/AppTable";

import UnitStatusDot from "./UnitStatusDot";
import { MobileExpandedDetails, UnitTableView } from "./UnitViews";

interface ServiceTableViewProps {
  onDoubleClick?: (name: string) => void;
  onSelect?: (name: string | null) => void;
  selected?: string | null;
  services: Service[];
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

const renderServiceMainRow = (service: Service, isMobile: boolean) => (
  <>
    <AppTableCell style={{ paddingLeft: 8 }}>
      <UnitStatusDot activeState={service.active_state} />
      {service.active_state}
    </AppTableCell>
    <AppTableCell>{service.name}</AppTableCell>
    {!isMobile && (
      <>
        <AppTableCell>{service.load_state}</AppTableCell>
        <AppTableCell>{service.sub_state}</AppTableCell>
        <AppTableCell>{service.description || "-"}</AppTableCell>
      </>
    )}
  </>
);

const renderServiceMobileExpandedContent = (service: Service) => (
  <MobileExpandedDetails
    rows={[
      { label: "Load", value: service.load_state },
      { label: "Sub", value: service.sub_state },
      { label: "Description", value: service.description || "—" },
    ]}
  />
);

const ServiceTableView = ({
  services,
  selected,
  onSelect,
  onDoubleClick,
}: ServiceTableViewProps) => {
  const handleDoubleClick = useCallback(
    (key: string | number) => {
      if (typeof key === "string") {
        onDoubleClick?.(key);
      }
    },
    [onDoubleClick],
  );
  const handleSelect = useCallback(
    (key: string | number | null) =>
      onSelect?.(typeof key === "string" ? key : null),
    [onSelect],
  );

  return (
    <UnitTableView
      data={services}
      desktopColumns={desktopColumns}
      emptyMessage="No services found."
      getRowKey={getServiceRowKey}
      mobileColumns={mobileColumns}
      onDoubleClick={handleDoubleClick}
      onSelect={handleSelect}
      renderMainRow={renderServiceMainRow}
      renderMobileExpandedContent={renderServiceMobileExpandedContent}
      selected={selected}
    />
  );
};

export default ServiceTableView;
