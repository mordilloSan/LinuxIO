import FolderIcon from "@mui/icons-material/Folder";
import { Box, TableCell, TextField, Chip, Typography } from "@mui/material";
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

const VolumeList: React.FC = () => {
  const { data: volumes = [] } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");

  // Ensure volumes is an array (handle null/undefined from API)
  const volumesList = Array.isArray(volumes) ? volumes : [];

  const filtered = volumesList.filter(
    (vol) =>
      vol.Name.toLowerCase().includes(search.toLowerCase()) ||
      vol.Driver.toLowerCase().includes(search.toLowerCase()) ||
      vol.Mountpoint?.toLowerCase().includes(search.toLowerCase()),
  );

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Volume Name", align: "left" },
    {
      field: "driver",
      headerName: "Driver",
      align: "left",
      width: "120px",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    {
      field: "mountpoint",
      headerName: "Mountpoint",
      align: "left",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "scope",
      headerName: "Scope",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2}>
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search volumes…"
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
        getRowKey={(volume) => volume.Name}
        renderFirstCell={() => (
          <FolderIcon fontSize="small" sx={{ opacity: 0.7 }} />
        )}
        renderMainRow={(volume) => (
          <>
            <TableCell>
              <Typography
                variant="body2"
                fontWeight="medium"
                sx={responsiveTextStyles}
              >
                {volume.Name}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Chip
                label={volume.Driver}
                size="small"
                sx={{ fontSize: "0.75rem" }}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  ...longTextStyles,
                }}
              >
                {volume.Mountpoint || "-"}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {volume.Scope || "local"}
              </Typography>
            </TableCell>
          </>
        )}
        renderExpandedContent={(volume) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Full Mountpoint:</b>
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
              {volume.Mountpoint || "-"}
            </Typography>

            {volume.CreatedAt && (
              <>
                <Typography variant="subtitle2" gutterBottom>
                  <b>Created:</b>
                </Typography>
                <Typography variant="body2" sx={{ mb: 2, fontSize: "0.85rem" }}>
                  {new Date(volume.CreatedAt).toLocaleString()}
                </Typography>
              </>
            )}

            <Typography variant="subtitle2" gutterBottom>
              <b>Labels:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {volume.Labels && Object.keys(volume.Labels).length > 0 ? (
                Object.entries(volume.Labels).map(([key, val]) => (
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
              <b>Options:</b>
            </Typography>
            <Box>
              {volume.Options && Object.keys(volume.Options).length > 0 ? (
                Object.entries(volume.Options).map(([key, val]) => (
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
          </>
        )}
        emptyMessage="No volumes found."
      />
    </Box>
  );
};

export default VolumeList;
