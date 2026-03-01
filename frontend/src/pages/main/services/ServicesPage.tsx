import GridViewIcon from "@mui/icons-material/GridView";
import TableRowsIcon from "@mui/icons-material/TableRows";
import { Alert, Box, IconButton, TextField, Tooltip } from "@mui/material";
import React, { useState, useEffect } from "react";

import ServiceCardsView from "./ServiceCardsView";
import ServiceTableView from "./ServiceTableView";

import { linuxio } from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useViewMode } from "@/hooks/useViewMode";

const ServicesPage: React.FC = () => {
  const {
    data,
    isPending: isLoading,
    isError,
    error,
  } = linuxio.dbus.list_services.useQuery({
    refetchInterval: 2000,
  });

  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("services.list", "table");
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setExpanded(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const filtered = (data ?? []).filter(
    (s) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      (s.description?.toLowerCase().includes(search.toLowerCase()) ?? false),
  );

  return (
    <Box>
      {isLoading && <ComponentLoader />}
      {isError && (
        <Alert severity="error">
          {error instanceof Error ? error.message : "Failed to load services"}
        </Alert>
      )}
      {data && (
        <>
          <Box mb={2} display="flex" alignItems="center" gap={2}>
            <TextField
              variant="outlined"
              size="small"
              placeholder="Search services…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              sx={{ width: 320 }}
            />
            <Box fontWeight="bold">{filtered.length} shown</Box>
            <Tooltip
              title={
                viewMode === "table"
                  ? "Switch to card view"
                  : "Switch to table view"
              }
            >
              <IconButton
                size="small"
                onClick={() => {
                  setViewMode(viewMode === "table" ? "card" : "table");
                  setExpanded(null);
                }}
              >
                {viewMode === "table" ? (
                  <GridViewIcon fontSize="small" />
                ) : (
                  <TableRowsIcon fontSize="small" />
                )}
              </IconButton>
            </Tooltip>
          </Box>

          {viewMode === "card" ? (
            <ServiceCardsView
              services={filtered}
              expanded={expanded}
              onExpand={setExpanded}
            />
          ) : (
            <ServiceTableView services={filtered} />
          )}
        </>
      )}
    </Box>
  );
};

export default ServicesPage;
