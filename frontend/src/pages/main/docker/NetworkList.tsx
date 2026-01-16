import HubIcon from "@mui/icons-material/Hub";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Chip,
  Typography,
} from "@mui/material";
import React, { useState } from "react";

import linuxio from "@/api/react-query";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";

const NetworkList: React.FC = () => {
  const { data: networks = [] } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");

  const filtered = networks.filter(
    (net) =>
      net.Name.toLowerCase().includes(search.toLowerCase()) ||
      net.Driver.toLowerCase().includes(search.toLowerCase()) ||
      net.Scope.toLowerCase().includes(search.toLowerCase()),
  );

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Network Name", align: "left" },
    { field: "driver", headerName: "Driver", align: "left", width: "120px" },
    {
      field: "scope",
      headerName: "Scope",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "internal",
      headerName: "Internal",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "ipv4",
      headerName: "IPv4",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", lg: "table-cell" } },
    },
    {
      field: "ipv6",
      headerName: "IPv6",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", lg: "table-cell" } },
    },
    {
      field: "id",
      headerName: "Network ID",
      align: "left",
      width: "140px",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2}>
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search networks…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{
            width: 320,
            "@media (max-width: 600px)": {
              width: "100%",
            },
          }}
        />
        <Box fontWeight="bold">{filtered.length} shown</Box>
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(network) => network.Id}
        renderFirstCell={() => (
          <HubIcon fontSize="small" sx={{ opacity: 0.7 }} />
        )}
        renderMainRow={(network) => (
          <>
            <TableCell>
              <Typography
                variant="body2"
                fontWeight="medium"
                sx={responsiveTextStyles}
              >
                {network.Name}
              </Typography>
            </TableCell>
            <TableCell>
              <Chip
                label={network.Driver}
                size="small"
                sx={{ fontSize: "0.75rem" }}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {network.Scope}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Chip
                label={network.Internal ? "Yes" : "No"}
                size="small"
                color={network.Internal ? "warning" : "default"}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
              <Chip
                label={network.EnableIPv4 !== false ? "Yes" : "No"}
                size="small"
                color={network.EnableIPv4 !== false ? "success" : "default"}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
              <Chip
                label={network.EnableIPv6 ? "Yes" : "No"}
                size="small"
                color={network.EnableIPv6 ? "success" : "default"}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  ...responsiveTextStyles,
                }}
              >
                {network.Id?.slice(0, 12)}
              </Typography>
            </TableCell>
          </>
        )}
        renderExpandedContent={(network) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Full Network ID:</b>
            </Typography>
            <Typography
              variant="body2"
              sx={{
                fontFamily: "monospace",
                fontSize: "0.85rem",
                mb: 2,
                ...longTextStyles,
              }}
            >
              {network.Id}
            </Typography>

            <Typography variant="subtitle2" gutterBottom>
              <b>Subnet(s):</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {network.IPAM?.Config && network.IPAM.Config.length > 0 ? (
                network.IPAM.Config.map((ipam, i) => (
                  <Chip
                    key={i}
                    label={`${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no IPAM config)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Options:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {network.Options && Object.keys(network.Options).length > 0 ? (
                Object.entries(network.Options).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no options)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Labels:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {network.Labels && Object.keys(network.Labels).length > 0 ? (
                Object.entries(network.Labels).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no labels)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Connected Containers:</b>
            </Typography>
            <Box>
              {network.Containers &&
              Object.keys(network.Containers).length > 0 ? (
                <Table
                  size="small"
                  sx={{
                    bgcolor: (theme) =>
                      theme.palette.mode === "dark"
                        ? "rgba(0,0,0,0.2)"
                        : "rgba(255,255,255,0.5)",
                    overflowX: "auto",
                    display: "block",
                  }}
                >
                  <TableHead>
                    <TableRow>
                      <TableCell>
                        <b>Name</b>
                      </TableCell>
                      <TableCell>
                        <b>Container ID</b>
                      </TableCell>
                      <TableCell>
                        <b>IPv4</b>
                      </TableCell>
                      <TableCell>
                        <b>IPv6</b>
                      </TableCell>
                      <TableCell>
                        <b>MAC</b>
                      </TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {Object.entries(network.Containers).map(
                      ([id, info]: [string, any]) => (
                        <TableRow key={id}>
                          <TableCell>
                            <Typography variant="body2" sx={responsiveTextStyles}>
                              {info.Name || "-"}
                            </Typography>
                          </TableCell>
                          <TableCell
                            sx={{
                              fontFamily: "monospace",
                              fontSize: "0.85rem",
                              ...longTextStyles,
                            }}
                          >
                            {id.slice(0, 12)}
                          </TableCell>
                          <TableCell>
                            <Typography
                              variant="body2"
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {info.IPv4Address?.replace(/\/.*/, "") || "-"}
                            </Typography>
                          </TableCell>
                          <TableCell>
                            <Typography
                              variant="body2"
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {info.IPv6Address?.replace(/\/.*/, "") || "-"}
                            </Typography>
                          </TableCell>
                          <TableCell
                            sx={{
                              fontFamily: "monospace",
                              fontSize: "0.85rem",
                              ...longTextStyles,
                            }}
                          >
                            {info.MacAddress || "-"}
                          </TableCell>
                        </TableRow>
                      ),
                    )}
                  </TableBody>
                </Table>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no containers)
                </Typography>
              )}
            </Box>
          </>
        )}
        emptyMessage="No networks found."
      />
    </Box>
  );
};

export default NetworkList;
