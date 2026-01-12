import {
  Box,
  Typography,
  Chip,
  Table,
  TableBody,
  TableCell,
  TableRow,
  TableHead,
  Paper,
} from "@mui/material";

import linuxio from "@/api/react-query";
import CollapsibleTable from "@/components/tables/CollapsibleTable";
import { CollapsibleColumn } from "@/types/collapsible";

const formatNetworkRows = (networks: any[]) =>
  networks.map((nw) => ({
    name: nw.Name,
    driver: nw.Driver,
    scope: nw.Scope,
    internal: nw.Internal ? "Yes" : "No",
    ipv4: nw.EnableIPv4 ? "Yes" : "No",
    ipv6: nw.EnableIPv6 ? "Yes" : "No",
    id: nw.Id?.slice(0, 12),
    raw: nw,
  }));

const networkColumns: CollapsibleColumn[] = [
  { field: "name", headerName: "Name", align: "left" },
  { field: "driver", headerName: "Driver", align: "left" },
  { field: "scope", headerName: "Scope", align: "left" },
  { field: "internal", headerName: "Internal", align: "center" },
  { field: "ipv4", headerName: "IPv4", align: "center" },
  { field: "ipv6", headerName: "IPv6", align: "center" },
  { field: "id", headerName: "Network ID", align: "left" },
];

function renderCollapseContent(row: any) {
  const nw = row.raw;
  const containers = nw.Containers && Object.entries(nw.Containers);

  return (
    <Box>
      <Typography variant="subtitle2">Subnet(s):</Typography>
      <Box sx={{ mb: 2 }}>
        {nw.IPAM?.Config && nw.IPAM.Config.length > 0 ? (
          nw.IPAM.Config.map((ipam: any, i: number) => (
            <Chip
              key={i}
              label={`Subnet: ${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
              size="small"
              sx={{ mr: 1, mb: 1 }}
            />
          ))
        ) : (
          <Typography variant="body2" color="text.secondary">
            (no IPAM config)
          </Typography>
        )}
      </Box>

      <Typography variant="subtitle2">Options:</Typography>
      <Box sx={{ mb: 2 }}>
        {nw.Options && Object.keys(nw.Options).length > 0 ? (
          Object.entries(nw.Options).map(([key, val]) => (
            <Chip
              key={key}
              label={`${key}: ${String(val)}`}
              size="small"
              sx={{ mr: 1, mb: 1 }}
            />
          ))
        ) : (
          <Typography variant="body2" color="text.secondary">
            (no options)
          </Typography>
        )}
      </Box>

      <Typography variant="subtitle2">Labels:</Typography>
      <Box sx={{ mb: 2 }}>
        {nw.Labels && Object.keys(nw.Labels).length > 0 ? (
          Object.entries(nw.Labels).map(([key, val]) => (
            <Chip
              key={key}
              label={`${key}: ${String(val)}`}
              size="small"
              sx={{ mr: 1, mb: 1 }}
            />
          ))
        ) : (
          <Typography variant="body2" color="text.secondary">
            (no labels)
          </Typography>
        )}
      </Box>

      <Typography variant="subtitle2">Containers:</Typography>
      <Box>
        {containers && containers.length > 0 ? (
          <Paper sx={{ mb: 2, background: "transparent", boxShadow: "none" }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  {["Name", "Container ID", "IPv4", "IPv6", "MAC"].map(
                    (header) => (
                      <TableCell
                        key={header}
                        sx={{ borderBottom: "none", background: "transparent" }}
                      >
                        <Chip label={header} size="small" />
                      </TableCell>
                    ),
                  )}
                </TableRow>
              </TableHead>

              <TableBody>
                {containers.map(([id, info]: [string, any]) => (
                  <TableRow key={id}>
                    <TableCell>{info.Name || "-"}</TableCell>
                    <TableCell>{id}</TableCell>
                    <TableCell>
                      {info.IPv4Address?.replace(/\/.*/, "") || "-"}
                    </TableCell>
                    <TableCell>
                      {info.IPv6Address?.replace(/\/.*/, "") || "-"}
                    </TableCell>
                    <TableCell>{info.MacAddress || "-"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Paper>
        ) : (
          <Typography variant="body2" color="text.secondary">
            (no containers)
          </Typography>
        )}
      </Box>
    </Box>
  );
}

export default function DockerNetworksTable() {
  const { data = [] } = linuxio.docker.list_networks.useQuery();
  const rows = formatNetworkRows(data);

  return (
    <CollapsibleTable
      rows={rows}
      columns={networkColumns}
      renderCollapseContent={renderCollapseContent}
    />
  );
}
