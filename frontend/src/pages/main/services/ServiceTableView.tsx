import { Box, TableCell, useMediaQuery, useTheme } from "@mui/material";
import React from "react";

import type { Service } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { getServiceStatusColor } from "@/constants/statusColors";

interface ServiceTableViewProps {
  services: Service[];
  selected?: string | null;
  onSelect?: (name: string | null) => void;
  onDoubleClick?: (name: string) => void;
}

const desktopColumns: UnifiedTableColumn[] = [
  {
    field: "status",
    headerName: "Status",
    align: "left",
    width: "120px",
    sx: { paddingLeft: "8px" },
  },
  { field: "name", headerName: "Name", align: "left", width: "200px" },
  { field: "load_state", headerName: "Load State", align: "left", width: "120px" },
  { field: "sub_state", headerName: "Sub State", align: "left", width: "120px" },
  { field: "description", headerName: "Description", align: "left" },
];

const mobileColumns: UnifiedTableColumn[] = [
  {
    field: "status",
    headerName: "Status",
    align: "left",
    width: "110px",
    sx: { paddingLeft: "8px" },
  },
  { field: "name", headerName: "Name", align: "left" },
];

const statusDot = (activeState: string) => (
  <Box
    component="span"
    sx={{
      display: "inline-block",
      width: 10,
      height: 10,
      borderRadius: "50%",
      bgcolor: getServiceStatusColor(activeState),
      mr: 1,
      flexShrink: 0,
    }}
  />
);

const ServiceTableView: React.FC<ServiceTableViewProps> = ({
  services,
  selected,
  onSelect,
  onDoubleClick,
}) => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

  return (
    <UnifiedCollapsibleTable
      data={services}
      columns={isMobile ? mobileColumns : desktopColumns}
      getRowKey={(service) => service.name}
      selectedKey={selected}
      onRowClick={
        isMobile
          ? undefined
          : (service) => onSelect?.(selected === service.name ? null : service.name)
      }
      onRowDoubleClick={(service) => onDoubleClick?.(service.name)}
      renderExpandedContent={
        isMobile
          ? (service) => (
              <div style={{ display: "flex", flexDirection: "column", gap: 6, padding: "2px 0" }}>
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
            )
          : undefined
      }
      renderMainRow={(service) => (
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
};

export default ServiceTableView;
