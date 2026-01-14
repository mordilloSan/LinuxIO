import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import FolderIcon from "@mui/icons-material/Folder";
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

interface DockerVolume {
  Name: string;
  Driver: string;
  Mountpoint: string;
  CreatedAt?: string;
  Labels?: Record<string, string>;
  Options?: Record<string, string>;
  Scope?: string;
}

const VolumeList: React.FC = () => {
  const { data: volumes = [] } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  // Ensure volumes is an array (handle null/undefined from API)
  const volumesList = Array.isArray(volumes) ? volumes : [];

  const filtered = volumesList.filter(
    (vol) =>
      vol.Name.toLowerCase().includes(search.toLowerCase()) ||
      vol.Driver.toLowerCase().includes(search.toLowerCase()) ||
      vol.Mountpoint?.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2}>
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search volumes…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{ width: 320 }}
        />
        <Box fontWeight="bold">{filtered.length} shown</Box>
      </Box>
      <TableContainer>
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow
              sx={(theme) => ({
                "& .MuiTableCell-root": { borderBottom: "none" },
                backgroundColor:
                  theme.palette.mode === "dark"
                    ? "rgba(255,255,255,0.08)"
                    : "rgba(0,0,0,0.08)",
                borderRadius: "6px",
                boxShadow: "none",
              })}
            >
              <TableCell>Volume Name</TableCell>
              <TableCell>Driver</TableCell>
              <TableCell>Mountpoint</TableCell>
              <TableCell>Scope</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {filtered.map((volume, index) => (
              <React.Fragment key={volume.Name}>
                <TableRow
                  sx={(theme) => ({
                    "& .MuiTableCell-root": { borderBottom: "none" },
                    backgroundColor:
                      index % 2 === 0
                        ? "transparent"
                        : theme.palette.mode === "dark"
                          ? "rgba(255,255,255,0.04)"
                          : "rgba(0,0,0,0.05)",
                  })}
                >
                  <TableCell>
                    <Box sx={{ display: "flex", alignItems: "center" }}>
                      <FolderIcon
                        fontSize="small"
                        sx={{ mr: 1, opacity: 0.7 }}
                      />
                      <Typography variant="body2" fontWeight="medium">
                        {volume.Name}
                      </Typography>
                    </Box>
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={volume.Driver}
                      size="small"
                      sx={{ fontSize: "0.75rem" }}
                    />
                  </TableCell>
                  <TableCell>
                    <Typography
                      variant="body2"
                      noWrap
                      sx={{
                        maxWidth: 300,
                        fontFamily: "monospace",
                        fontSize: "0.85rem",
                      }}
                    >
                      {volume.Mountpoint || "-"}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2">
                      {volume.Scope || "local"}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <IconButton
                      size="small"
                      onClick={() =>
                        setExpanded(
                          expanded === volume.Name ? null : volume.Name,
                        )
                      }
                    >
                      <ExpandMoreIcon
                        style={{
                          transform:
                            expanded === volume.Name
                              ? "rotate(180deg)"
                              : "rotate(0deg)",
                          transition: "0.2s",
                        }}
                      />
                    </IconButton>
                  </TableCell>
                </TableRow>
                <TableRow
                  sx={(theme) => ({
                    "& .MuiTableCell-root": { borderBottom: "none" },
                    backgroundColor:
                      index % 2 === 0
                        ? "transparent"
                        : theme.palette.mode === "dark"
                          ? "rgba(255,255,255,0.08)"
                          : "rgba(0,0,0,0.05)",
                  })}
                >
                  <TableCell
                    style={{ paddingBottom: 0, paddingTop: 0 }}
                    colSpan={5}
                  >
                    <Collapse
                      in={expanded === volume.Name}
                      timeout="auto"
                      unmountOnExit
                    >
                      <Box
                        component={motion.div}
                        initial={{ opacity: 0, y: -10 }}
                        animate={{ opacity: 1, y: 0 }}
                        sx={{
                          margin: 2,
                          borderRadius: 2,
                          p: 2,
                          bgcolor: (theme) =>
                            theme.palette.mode === "dark"
                              ? "rgba(255,255,255,0.05)"
                              : "rgba(0,0,0,0.03)",
                        }}
                      >
                        <Typography variant="subtitle2" gutterBottom>
                          <b>Full Mountpoint:</b>
                        </Typography>
                        <Typography
                          variant="body2"
                          sx={{
                            fontFamily: "monospace",
                            fontSize: "0.85rem",
                            mb: 2,
                            wordBreak: "break-all",
                          }}
                        >
                          {volume.Mountpoint || "-"}
                        </Typography>

                        {volume.CreatedAt && (
                          <>
                            <Typography variant="subtitle2" gutterBottom>
                              <b>Created:</b>
                            </Typography>
                            <Typography
                              variant="body2"
                              sx={{ mb: 2, fontSize: "0.85rem" }}
                            >
                              {new Date(volume.CreatedAt).toLocaleString()}
                            </Typography>
                          </>
                        )}

                        <Typography variant="subtitle2" gutterBottom>
                          <b>Labels:</b>
                        </Typography>
                        <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
                          {volume.Labels &&
                          Object.keys(volume.Labels).length > 0 ? (
                            Object.entries(volume.Labels).map(([key, val]) => (
                              <Chip
                                key={key}
                                label={`${key}: ${val}`}
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

                        <Typography variant="subtitle2" gutterBottom>
                          <b>Options:</b>
                        </Typography>
                        <Box>
                          {volume.Options &&
                          Object.keys(volume.Options).length > 0 ? (
                            Object.entries(volume.Options).map(([key, val]) => (
                              <Chip
                                key={key}
                                label={`${key}: ${val}`}
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
                      </Box>
                    </Collapse>
                  </TableCell>
                </TableRow>
              </React.Fragment>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {filtered.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            No volumes found.
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default VolumeList;
