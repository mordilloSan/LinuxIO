import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import HubIcon from "@mui/icons-material/Hub";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  TextField,
  Collapse,
  Chip,
  Typography,
} from "@mui/material";
import { motion } from "framer-motion";
import React, { useState } from "react";

import linuxio from "@/api/react-query";
import {
  getTableHeaderStyles,
  getTableRowStyles,
  getExpandedRowStyles,
  getExpandedContentStyles,
  tableContainerStyles,
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";

const NetworkList: React.FC = () => {
  const { data: networks = [] } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const filtered = networks.filter(
    (net) =>
      net.Name.toLowerCase().includes(search.toLowerCase()) ||
      net.Driver.toLowerCase().includes(search.toLowerCase()) ||
      net.Scope.toLowerCase().includes(search.toLowerCase()),
  );

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
      <TableContainer sx={tableContainerStyles}>
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow sx={getTableHeaderStyles}>
              <TableCell>Network Name</TableCell>
              <TableCell>Driver</TableCell>
              <TableCell>Scope</TableCell>
              <TableCell align="center">Internal</TableCell>
              <TableCell align="center">IPv4</TableCell>
              <TableCell align="center">IPv6</TableCell>
              <TableCell>Network ID</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {filtered.map((network, index) => {
              const rowStyles = (theme: any) => getTableRowStyles(theme, index);
              const expandedRowStyles = (theme: any) =>
                getExpandedRowStyles(theme, index);
              return (
                <React.Fragment key={network.Id}>
                  <TableRow sx={rowStyles}>
                    <TableCell>
                      <Box sx={{ display: "flex", alignItems: "center" }}>
                        <HubIcon
                          fontSize="small"
                          sx={{ mr: 1, opacity: 0.7 }}
                        />
                        <Typography
                          variant="body2"
                          fontWeight="medium"
                          sx={responsiveTextStyles}
                        >
                          {network.Name}
                        </Typography>
                      </Box>
                    </TableCell>
                    <TableCell>
                      <Chip
                        label={network.Driver}
                        size="small"
                        sx={{ fontSize: "0.75rem" }}
                      />
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" sx={responsiveTextStyles}>
                        {network.Scope}
                      </Typography>
                    </TableCell>
                    <TableCell align="center">
                      <Chip
                        label={network.Internal ? "Yes" : "No"}
                        size="small"
                        color={network.Internal ? "warning" : "default"}
                      />
                    </TableCell>
                    <TableCell align="center">
                      <Chip
                        label={network.EnableIPv4 !== false ? "Yes" : "No"}
                        size="small"
                        color={
                          network.EnableIPv4 !== false ? "success" : "default"
                        }
                      />
                    </TableCell>
                    <TableCell align="center">
                      <Chip
                        label={network.EnableIPv6 ? "Yes" : "No"}
                        size="small"
                        color={network.EnableIPv6 ? "success" : "default"}
                      />
                    </TableCell>
                    <TableCell>
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
                    <TableCell>
                      <IconButton
                        size="small"
                        onClick={() =>
                          setExpanded(
                            expanded === network.Id ? null : network.Id,
                          )
                        }
                      >
                        <ExpandMoreIcon
                          style={{
                            transform:
                              expanded === network.Id
                                ? "rotate(180deg)"
                                : "rotate(0deg)",
                            transition: "0.2s",
                          }}
                        />
                      </IconButton>
                    </TableCell>
                  </TableRow>
                  <TableRow sx={expandedRowStyles}>
                    <TableCell
                      style={{ paddingBottom: 0, paddingTop: 0 }}
                      colSpan={8}
                    >
                      <Collapse
                        in={expanded === network.Id}
                        timeout="auto"
                        unmountOnExit
                      >
                        <Box
                          component={motion.div}
                          initial={{ opacity: 0, y: -10 }}
                          animate={{ opacity: 1, y: 0 }}
                          sx={getExpandedContentStyles}
                        >
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
                          <Box
                            sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}
                          >
                            {network.IPAM?.Config &&
                            network.IPAM.Config.length > 0 ? (
                              network.IPAM.Config.map((ipam, i) => (
                                <Chip
                                  key={i}
                                  label={`${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
                                  size="small"
                                  sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                                />
                              ))
                            ) : (
                              <Typography
                                variant="body2"
                                color="text.secondary"
                              >
                                (no IPAM config)
                              </Typography>
                            )}
                          </Box>

                          <Typography variant="subtitle2" gutterBottom>
                            <b>Options:</b>
                          </Typography>
                          <Box
                            sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}
                          >
                            {network.Options &&
                            Object.keys(network.Options).length > 0 ? (
                              Object.entries(network.Options).map(
                                ([key, val]) => (
                                  <Chip
                                    key={key}
                                    label={`${key}: ${val}`}
                                    size="small"
                                    sx={{
                                      mr: 1,
                                      mb: 1,
                                      ...wrappableChipStyles,
                                    }}
                                  />
                                ),
                              )
                            ) : (
                              <Typography
                                variant="body2"
                                color="text.secondary"
                              >
                                (no options)
                              </Typography>
                            )}
                          </Box>

                          <Typography variant="subtitle2" gutterBottom>
                            <b>Labels:</b>
                          </Typography>
                          <Box
                            sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}
                          >
                            {network.Labels &&
                            Object.keys(network.Labels).length > 0 ? (
                              Object.entries(network.Labels).map(
                                ([key, val]) => (
                                  <Chip
                                    key={key}
                                    label={`${key}: ${val}`}
                                    size="small"
                                    sx={{
                                      mr: 1,
                                      mb: 1,
                                      ...wrappableChipStyles,
                                    }}
                                  />
                                ),
                              )
                            ) : (
                              <Typography
                                variant="body2"
                                color="text.secondary"
                              >
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
                                          <Typography
                                            variant="body2"
                                            sx={responsiveTextStyles}
                                          >
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
                                            {info.IPv4Address?.replace(
                                              /\/.*/,
                                              "",
                                            ) || "-"}
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
                                            {info.IPv6Address?.replace(
                                              /\/.*/,
                                              "",
                                            ) || "-"}
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
                              <Typography
                                variant="body2"
                                color="text.secondary"
                              >
                                (no containers)
                              </Typography>
                            )}
                          </Box>
                        </Box>
                      </Collapse>
                    </TableCell>
                  </TableRow>
                </React.Fragment>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
      {filtered.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            No networks found.
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default NetworkList;
