import {
  Alert,
  Box,
  TableCell,
  TextField,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import React, { useMemo, useState } from "react";

import type { Timer } from "@/api";
import { linuxio } from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { getServiceStatusColor } from "@/constants/statusColors";

// Formats a microsecond Unix timestamp to a human-readable string.
// Returns "—" for 0 or UINT64_MAX (systemd's "never/unknown" sentinel).
const UINT64_MAX_USEC = 18446744073709551615n;
function formatUsec(usec: number): string {
  if (!usec || usec === 0 || usec >= UINT64_MAX_USEC) return "—";
  const d = new Date(usec / 1000);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString();
}

const desktopColumns: UnifiedTableColumn[] = [
  {
    field: "status",
    headerName: "Status",
    align: "left",
    width: "120px",
    sx: { paddingLeft: "8px" },
  },
  { field: "name", headerName: "Name", align: "left", width: "220px" },
  { field: "unit", headerName: "Unit", align: "left", width: "220px" },
  {
    field: "next_elapse",
    headerName: "Next Elapse",
    align: "left",
    width: "180px",
  },
  { field: "last_trigger", headerName: "Last Trigger", align: "left" },
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

function compareTimersByName(a: Timer, b: Timer): number {
  return a.name.localeCompare(b.name, undefined, {
    numeric: true,
    sensitivity: "base",
  });
}

const TimersTab: React.FC = () => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

  const { data, isPending, isError, error } = linuxio.dbus.list_timers.useQuery(
    {
      refetchInterval: 5000,
    },
  );

  const [search, setSearch] = useState("");

  const filtered = useMemo(
    () =>
      (data ?? [])
        .filter(
          (t) =>
            t.name.toLowerCase().includes(search.toLowerCase()) ||
            (t.description?.toLowerCase().includes(search.toLowerCase()) ??
              false) ||
            t.unit.toLowerCase().includes(search.toLowerCase()),
        )
        .sort(compareTimersByName),
    [data, search],
  );

  return (
    <Box>
      {isPending && <ComponentLoader />}
      {isError && (
        <Alert severity="error">
          {error instanceof Error ? error.message : "Failed to load timers"}
        </Alert>
      )}
      {data && (
        <>
          <Box mb={2} display="flex" alignItems="center" gap={2}>
            <TextField
              variant="outlined"
              size="small"
              placeholder="Search timers…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              sx={{ width: 320 }}
            />
            <Box fontWeight="bold">{filtered.length} shown</Box>
          </Box>

          <UnifiedCollapsibleTable<Timer>
            data={filtered}
            columns={isMobile ? mobileColumns : desktopColumns}
            getRowKey={(t) => t.name}
            renderMainRow={(t) => (
              <>
                <TableCell sx={{ paddingLeft: "8px" }}>
                  {statusDot(t.active_state)}
                  {t.active_state}
                </TableCell>
                <TableCell>{t.name}</TableCell>
                {!isMobile && (
                  <>
                    <TableCell>{t.unit || "—"}</TableCell>
                    <TableCell>{formatUsec(t.next_elapse_usec)}</TableCell>
                    <TableCell>{formatUsec(t.last_trigger_usec)}</TableCell>
                  </>
                )}
              </>
            )}
            renderExpandedContent={
              isMobile
                ? (t) => (
                    <div
                      style={{
                        display: "flex",
                        flexDirection: "column",
                        gap: 6,
                        padding: "2px 0",
                      }}
                    >
                      {[
                        { label: "Unit", value: t.unit || "—" },
                        {
                          label: "Next",
                          value: formatUsec(t.next_elapse_usec),
                        },
                        {
                          label: "Last",
                          value: formatUsec(t.last_trigger_usec),
                        },
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
            emptyMessage="No timers found."
          />
        </>
      )}
    </Box>
  );
};

export default TimersTab;
