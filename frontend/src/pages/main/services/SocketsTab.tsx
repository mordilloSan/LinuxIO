import {
  Alert,
  Box,
  Chip,
  TableCell,
  TextField,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import React, { useMemo, useState } from "react";

import type { Socket } from "@/api";
import { linuxio } from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { getServiceStatusColor } from "@/constants/statusColors";

const desktopColumns: UnifiedTableColumn[] = [
  {
    field: "status",
    headerName: "Status",
    align: "left",
    width: "120px",
    sx: { paddingLeft: "8px" },
  },
  { field: "name", headerName: "Name", align: "left", width: "220px" },
  { field: "listen", headerName: "Listen", align: "left" },
  {
    field: "connections",
    headerName: "Connections",
    align: "right",
    width: "130px",
  },
  { field: "accepted", headerName: "Accepted", align: "right", width: "120px" },
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

const SocketsTab: React.FC = () => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

  const { data, isPending, isError, error } =
    linuxio.dbus.list_sockets.useQuery({
      refetchInterval: 5000,
    });

  const [search, setSearch] = useState("");

  const filtered = useMemo(
    () =>
      (data ?? []).filter(
        (s) =>
          s.name.toLowerCase().includes(search.toLowerCase()) ||
          (s.description?.toLowerCase().includes(search.toLowerCase()) ??
            false) ||
          s.listen.some((addr) =>
            addr.toLowerCase().includes(search.toLowerCase()),
          ),
      ),
    [data, search],
  );

  return (
    <Box>
      {isPending && <ComponentLoader />}
      {isError && (
        <Alert severity="error">
          {error instanceof Error ? error.message : "Failed to load sockets"}
        </Alert>
      )}
      {data && (
        <>
          <Box mb={2} display="flex" alignItems="center" gap={2}>
            <TextField
              variant="outlined"
              size="small"
              placeholder="Search sockets…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              sx={{ width: 320 }}
            />
            <Box fontWeight="bold">{filtered.length} shown</Box>
          </Box>

          <UnifiedCollapsibleTable<Socket>
            data={filtered}
            columns={isMobile ? mobileColumns : desktopColumns}
            getRowKey={(s) => s.name}
            renderMainRow={(s) => (
              <>
                <TableCell sx={{ paddingLeft: "8px" }}>
                  {statusDot(s.active_state)}
                  {s.active_state}
                </TableCell>
                <TableCell>{s.name}</TableCell>
                {!isMobile && (
                  <>
                    <TableCell>
                      <Box display="flex" gap={0.5} flexWrap="wrap">
                        {s.listen.length > 0
                          ? s.listen.map((addr) => (
                              <Chip
                                key={addr}
                                label={addr}
                                size="small"
                                variant="outlined"
                              />
                            ))
                          : "—"}
                      </Box>
                    </TableCell>
                    <TableCell align="right">{s.n_connections}</TableCell>
                    <TableCell align="right">{s.n_accepted}</TableCell>
                  </>
                )}
              </>
            )}
            renderExpandedContent={
              isMobile
                ? (s) => (
                    <div
                      style={{
                        display: "flex",
                        flexDirection: "column",
                        gap: 6,
                        padding: "2px 0",
                      }}
                    >
                      {[
                        { label: "Listen", value: s.listen.join(", ") || "—" },
                        {
                          label: "Connections",
                          value: String(s.n_connections),
                        },
                        { label: "Accepted", value: String(s.n_accepted) },
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
                          <span style={{ fontSize: "0.8rem", fontWeight: 500 }}>
                            {value}
                          </span>
                        </div>
                      ))}
                    </div>
                  )
                : undefined
            }
            emptyMessage="No sockets found."
          />
        </>
      )}
    </Box>
  );
};

export default SocketsTab;
