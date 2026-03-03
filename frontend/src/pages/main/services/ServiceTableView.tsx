import { TableCell } from "@mui/material";
import React from "react";

import { UnitTableView, statusDot } from "./UnitViews";

import type { Service } from "@/api";

interface ServiceTableViewProps {
  services: Service[];
  selected?: string | null;
  onSelect?: (name: string | null) => void;
  onDoubleClick?: (name: string) => void;
}

const desktopColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "120px",
    sx: { paddingLeft: "8px" },
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
    sx: { paddingLeft: "8px" },
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
    mobileColumns={mobileColumns}
    getRowKey={(service) => service.name}
    selected={selected}
    onSelect={(key) => onSelect?.(typeof key === "string" ? key : null)}
    onDoubleClick={(key) => {
      if (typeof key === "string") {
        onDoubleClick?.(key);
      }
    }}
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
                color: "var(--mui-palette-text-secondary)",
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
    renderMainRow={(service, isMobile) => (
      <>
        <TableCell sx={{ paddingLeft: "8px" }}>
          {statusDot(service.active_state)}
          {service.active_state}
        </TableCell>
        <TableCell>{service.name}</TableCell>
        {!isMobile && (
          <>
            <TableCell>{service.load_state}</TableCell>
            <TableCell>{service.sub_state}</TableCell>
            <TableCell>{service.description || "-"}</TableCell>
          </>
        )}
      </>
    )}
    emptyMessage="No services found."
  />
);

export default ServiceTableView;
