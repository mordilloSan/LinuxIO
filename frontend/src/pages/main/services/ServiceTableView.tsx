import React from "react";

import { statusDot, UnitTableView } from "./UnitViews";

import type { Service } from "@/api";
import { AppTableCell } from "@/components/ui/AppTable";

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

const ServiceTableView: React.FC<ServiceTableViewProps> = ({
  services,
  selected,
  onSelect,
  onDoubleClick,
}) => (
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
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 6,
          padding: "2px 0",
        }}
      >
        {[
          { label: "Load", value: service.load_state },
          { label: "Sub", value: service.sub_state },
          { label: "Description", value: service.description || "—" },
        ].map(({ label, value }) => (
          <div key={label} style={{ display: "flex", gap: 12 }}>
            <span
              style={{
                fontSize: "0.6rem",
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                color: "var(--app-palette-text-secondary)",
                width: 80,
                flexShrink: 0,
                paddingTop: 2,
              }}
            >
              {label}
            </span>
            <span style={{ fontSize: "0.8rem", fontWeight: 500 }}>{value}</span>
          </div>
        ))}
      </div>
    )}
    selected={selected}
  />
);

export default ServiceTableView;
