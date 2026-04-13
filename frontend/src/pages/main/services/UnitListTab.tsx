import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useState,
} from "react";
import { useSearchParams } from "react-router-dom";

import type { UnitListItem } from "./UnitViews";

import PageLoader from "@/components/loaders/PageLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import { useAppTheme } from "@/theme";
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
  urlParam: string;
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
  urlParam,
}: UnitListTabProps<T>) {
  const theme = useAppTheme();
  const [search, setSearch] = useState("");
  const [searchParams, setSearchParams] = useSearchParams();
  const expanded = searchParams.get(urlParam);
  const setExpanded = useCallback(
    (name: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (name === null) {
            next.delete(urlParam);
          } else {
            next.set(urlParam, name);
          }
          return next;
        },
        { replace: false },
      );
    },
    [urlParam, setSearchParams],
  );
  const [returnToTable, setReturnToTable] = useState(false);

  const handleEscapeKey = useEffectEvent((event: KeyboardEvent) => {
    if (event.key !== "Escape") {
      return;
    }

    setExpanded(null);
    if (returnToTable) {
      setViewMode("table");
      setReturnToTable(false);
    }
  });

  useEffect(() => {
    window.addEventListener("keydown", handleEscapeKey);
    return () => window.removeEventListener("keydown", handleEscapeKey);
  }, []);

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
    <>
      {isPending && <PageLoader />}
      {isError && (
        <AppAlert severity="error">
          {error instanceof Error ? error.message : errorMessage}
        </AppAlert>
      )}
      {data !== undefined && (
        <>
          <div
            style={{
              marginBottom: theme.spacing(2),
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(2),
            }}
          >
            <AppSearchField
              placeholder={searchPlaceholder}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              style={{ width: 320 }}
            />
            <div style={{ fontWeight: "bold" }}>{filtered.length} shown</div>
          </div>

          {viewMode === "card" ? (
            renderCardsView({
              items: filtered,
              expanded,
              onExpand: handleCardExpand,
              renderDetailPanel: (item) =>
                renderDetailPanel(item, () => handleCardExpand(null)),
            })
          ) : (
            <AppGrid container spacing={3} alignItems="flex-start">
              <AppGrid size={{ xs: 12, md: selectedItem ? 7 : 12 }}>
                {renderTableView({
                  items: filtered,
                  selected: expanded,
                  onSelect: setExpanded,
                  onDoubleClick: handleOpenCardView,
                })}
              </AppGrid>
              {selectedItem && (
                <AppGrid size={{ xs: 12, md: 5 }}>
                  {renderDetailPanel(selectedItem, () => setExpanded(null))}
                </AppGrid>
              )}
            </AppGrid>
          )}
        </>
      )}
    </>
  );
}

export default UnitListTab;
