import { Box, TableCell } from "@mui/material";
import React from "react";

import type { Service } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { getServiceStatusColor } from "@/constants/statusColors";

interface ServiceTableViewProps {
  services: Service[];
}

const columns: UnifiedTableColumn[] = [
  {
    field: "status",
    headerName: "Status",
    align: "left",
    width: "120px",
    sx: { paddingLeft: "8px" },
  },
  { field: "name", headerName: "Name", align: "left", width: "200px" },
  {
    field: "load_state",
    headerName: "Load State",
    align: "left",
    width: "120px",
  },
  {
    field: "sub_state",
    headerName: "Sub State",
    align: "left",
    width: "120px",
  },
  { field: "description", headerName: "Description", align: "left" },
];

const ServiceTableView: React.FC<ServiceTableViewProps> = ({ services }) => (
  <UnifiedCollapsibleTable
    data={services}
    columns={columns}
    getRowKey={(service) => service.name}
    renderMainRow={(service) => (
      <>
        <TableCell sx={{ paddingLeft: "8px" }}>
          <Box
            component="span"
            sx={{
              display: "inline-block",
              width: 12,
              height: 12,
              borderRadius: "50%",
              bgcolor: getServiceStatusColor(service.active_state),
              mr: 1,
            }}
          />
          {service.active_state}
        </TableCell>
        <TableCell>{service.name}</TableCell>
        <TableCell>{service.load_state}</TableCell>
        <TableCell>{service.sub_state}</TableCell>
        <TableCell>{service.description || "-"}</TableCell>
      </>
    )}
    renderExpandedContent={(service) => (
      <>
        <b>Name:</b> {service.name}
        <br />
        <b>Description:</b> {service.description || "-"}
        <br />
        <b>Load State:</b> {service.load_state}
        <br />
        <b>Active State:</b> {service.active_state}
        <br />
        <b>Sub State:</b> {service.sub_state}
      </>
    )}
    emptyMessage="No services found."
  />
);

export default ServiceTableView;
