import { Alert, Box, TextField } from "@mui/material";
import React, { useEffect, useMemo, useState } from "react";

import type { UnitListItem } from "./UnitViews";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import type { TableCardViewMode } from "@/types/config";

interface UnitTableViewRenderProps<T> {
  items: T[];
  selected: string | null;
  onSelect: (name: string | null) => void;
  onDoubleClick: (name: string) => void;
}

interface UnitCardsViewRenderProps<T> {
  items: T[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (item: T) => React.ReactNode;
}

interface UnitListTabProps<T extends UnitListItem> {
  viewMode: TableCardViewMode;
  setViewMode: (
    next: TableCardViewMode | ((prev: TableCardViewMode) => TableCardViewMode),
  ) => void;
  data: T[] | undefined;
  isPending: boolean;
  isError: boolean;
  error: unknown;
  searchPlaceholder: string;
  errorMessage: string;
  compareItems: (a: T, b: T) => number;
  matchesSearch: (item: T, search: string) => boolean;
  renderTableView: (props: UnitTableViewRenderProps<T>) => React.ReactNode;
  renderCardsView: (props: UnitCardsViewRenderProps<T>) => React.ReactNode;
  renderDetailPanel: (item: T, onClose: () => void) => React.ReactNode;
}

function UnitListTab<T extends UnitListItem>({
  viewMode,
  setViewMode,
  data,
  isPending,
  isError,
  error,
  searchPlaceholder,
  errorMessage,
  compareItems,
  matchesSearch,
  renderTableView,
  renderCardsView,
  renderDetailPanel,
}: UnitListTabProps<T>) {
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [returnToTable, setReturnToTable] = useState(false);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }

      setExpanded(null);
      if (returnToTable) {
        setViewMode("table");
        setReturnToTable(false);
      }
    };

    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [returnToTable, setViewMode]);

  const filtered = useMemo(() => {
    const searchText = search.trim().toLowerCase();

    return (data ?? [])
      .filter((item) => matchesSearch(item, searchText))
      .sort(compareItems);
  }, [compareItems, data, matchesSearch, search]);

  const handleCardExpand = (name: string | null) => {
    setExpanded(name);
    if (name === null && returnToTable) {
      setViewMode("table");
      setReturnToTable(false);
    }
  };

  const handleOpenCardView = (name: string) => {
    setViewMode("card");
    setExpanded(name);
    setReturnToTable(true);
  };

  const selectedItem = expanded
    ? (filtered.find((item) => item.name === expanded) ?? null)
    : null;

  return (
    <Box>
      {isPending && <ComponentLoader />}
      {isError && (
        <Alert severity="error">
          {error instanceof Error ? error.message : errorMessage}
        </Alert>
      )}
      {data !== undefined && (
        <>
          <Box mb={2} display="flex" alignItems="center" gap={2}>
            <TextField
              variant="outlined"
              size="small"
              placeholder={searchPlaceholder}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              sx={{ width: 320 }}
            />
            <Box fontWeight="bold">{filtered.length} shown</Box>
          </Box>

          {viewMode === "card" ? (
            renderCardsView({
              items: filtered,
              expanded,
              onExpand: handleCardExpand,
              renderDetailPanel: (item) =>
                renderDetailPanel(item, () => handleCardExpand(null)),
            })
          ) : (
            <>
              {renderTableView({
                items: filtered,
                selected: expanded,
                onSelect: setExpanded,
                onDoubleClick: handleOpenCardView,
              })}
              {selectedItem && (
                <Box mt={3}>
                  {renderDetailPanel(selectedItem, () => setExpanded(null))}
                </Box>
              )}
            </>
          )}
        </>
      )}
    </Box>
  );
}

export default UnitListTab;
