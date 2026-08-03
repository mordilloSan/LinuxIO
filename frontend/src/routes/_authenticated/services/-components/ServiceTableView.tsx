import type { Service } from "@/api";
import { AppTableCell } from "@/components/ui/AppTable";

import { MobileExpandedDetails, statusDot, UnitTableView } from "./UnitViews";

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

const ServiceTableView = ({
  services,
  selected,
  onSelect,
  onDoubleClick,
}: ServiceTableViewProps) => (
  <UnitTableView
    data={services}
    desktopColumns={desktopColumns}
    emptyMessage="No services found."
    getRowKey={(service) => service.name}
    mobileColumns={mobileColumns}
    onDoubleClick={(key) => {
      if (typeof key === "string") {
        onDoubleClick?.(key);
      }
    }}
    onSelect={(key) => onSelect?.(typeof key === "string" ? key : null)}
    renderMainRow={(service, isMobile) => (
      <>
        <AppTableCell style={{ paddingLeft: 8 }}>
          {statusDot(service.active_state)}
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
    )}
    renderMobileExpandedContent={(service) => (
      <MobileExpandedDetails
        rows={[
          { label: "Load", value: service.load_state },
          { label: "Sub", value: service.sub_state },
          { label: "Description", value: service.description || "—" },
        ]}
      />
    )}
    selected={selected}
  />
);

export default ServiceTableView;
