import GridViewIcon from "@mui/icons-material/GridView";
import TableRowsIcon from "@mui/icons-material/TableRows";
import { Alert, Box, IconButton, TextField, Tooltip } from "@mui/material";
import React, { useState, useEffect, useMemo } from "react";

import ServiceCardsView from "./ServiceCardsView";
import ServiceDetailPanel from "./ServiceDetailPanel";
import ServiceTableView from "./ServiceTableView";
import SocketsTab from "./SocketsTab";
import TimersTab from "./TimersTab";

import { linuxio } from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import TabContainer from "@/components/tabbar/TabContainer";
import { useViewMode } from "@/hooks/useViewMode";

// Self-contained toggle that shares the same config key as ServicesTab
const ServicesViewToggle: React.FC = () => {
  const [viewMode, setViewMode] = useViewMode("services.list", "table");
  return (
    <Tooltip
      title={
        viewMode === "table" ? "Switch to card view" : "Switch to table view"
      }
    >
      <IconButton
        size="small"
        onClick={() => setViewMode(viewMode === "table" ? "card" : "table")}
      >
        {viewMode === "table" ? (
          <GridViewIcon fontSize="small" />
        ) : (
          <TableRowsIcon fontSize="small" />
        )}
      </IconButton>
    </Tooltip>
  );
};

const ServicesTab: React.FC = () => {
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
  const [returnToTable, setReturnToTable] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setExpanded(null);
        if (returnToTable) {
          setViewMode("table");
          setReturnToTable(false);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [returnToTable, setViewMode]);

  const filtered = useMemo(
    () =>
      (data ?? []).filter(
        (s) =>
          s.name.toLowerCase().includes(search.toLowerCase()) ||
          (s.description?.toLowerCase().includes(search.toLowerCase()) ??
            false),
      ),
    [data, search],
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
          </Box>

          {viewMode === "card" ? (
            <ServiceCardsView
              services={filtered}
              expanded={expanded}
              onExpand={(name) => {
                setExpanded(name);
                if (name === null && returnToTable) {
                  setViewMode("table");
                  setReturnToTable(false);
                }
              }}
            />
          ) : (
            <>
              <ServiceTableView
                services={filtered}
                selected={expanded}
                onSelect={setExpanded}
                onDoubleClick={(name) => {
                  setViewMode("card");
                  setExpanded(name);
                  setReturnToTable(true);
                }}
              />
              {expanded &&
                (() => {
                  const svc = filtered.find((s) => s.name === expanded);
                  return svc ? (
                    <Box mt={3}>
                      <ServiceDetailPanel
                        service={svc}
                        onClose={() => setExpanded(null)}
                      />
                    </Box>
                  ) : null;
                })()}
            </>
          )}
        </>
      )}
    </Box>
  );
};

const TABS = [
  {
    value: "services",
    label: "Services",
    component: <ServicesTab />,
    rightContent: <ServicesViewToggle />,
  },
  {
    value: "timers",
    label: "Timers",
    component: <TimersTab />,
  },
  {
    value: "sockets",
    label: "Sockets",
    component: <SocketsTab />,
  },
];

const ServicesPage: React.FC = () => (
  <TabContainer
    tabs={TABS}
    defaultTab="services"
    urlParam="section"
    containerSx={{ px: 0 }}
  />
);

export default ServicesPage;
