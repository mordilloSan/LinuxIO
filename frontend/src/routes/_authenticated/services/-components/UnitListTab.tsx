import { motion } from "framer-motion";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import type { TableCardViewMode } from "@/api";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import { useAppTheme } from "@/theme";
import {
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";

import type { UnitListItem } from "./UnitViews";

interface UnitTableViewRenderProps<T> {
  items: T[];
  onDoubleClick: (name: string) => void;
  onSelect: (name: string | null) => void;
  selected: string | null;
}

interface UnitCardsViewRenderProps<T> {
  expanded: string | null;
  items: T[];
  onExpand: (name: string | null) => void;
  renderDetailPanel: (item: T) => ReactNode;
}

interface UnitListTabProps<T extends UnitListItem> {
  compareItems: (a: T, b: T) => number;
  data: T[];
  matchesSearch: (item: T, search: string) => boolean;
  onSelectedChange: (name: string | null) => void;
  renderCardsView: (props: UnitCardsViewRenderProps<T>) => ReactNode;
  renderDetailPanel: (item: T, onClose: () => void) => ReactNode;
  renderTableView: (props: UnitTableViewRenderProps<T>) => ReactNode;
  searchPlaceholder: string;
  selected?: string;
  setViewMode: (
    next: TableCardViewMode | ((prev: TableCardViewMode) => TableCardViewMode),
  ) => void;
  viewMode: TableCardViewMode;
}

function UnitListTab<T extends UnitListItem>({
  viewMode,
  setViewMode,
  data,
  searchPlaceholder,
  compareItems,
  matchesSearch,
  renderTableView,
  renderCardsView,
  renderDetailPanel,
  selected,
  onSelectedChange,
}: UnitListTabProps<T>) {
  const theme = useAppTheme();
  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const [search, setSearch] = useState("");
  const expanded = selected;
  const setExpanded = useCallback(
    (name: string | null) => onSelectedChange(name),
    [onSelectedChange],
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

    return data
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

  const searchControls = (
    <div
      style={{
        marginBottom: theme.spacing(2),
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(2),
      }}
    >
      <AppSearchField
        onChange={(event) => setSearch(event.target.value)}
        placeholder={searchPlaceholder}
        style={{ width: 320 }}
        value={search}
      />
      <div style={{ fontWeight: "bold" }}>{filtered.length} shown</div>
    </div>
  );

  if (viewMode === "card") {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minHeight: 0,
          minWidth: 0,
        }}
      >
        {!selectedItem && searchControls}
        <div
          style={{
            display: "flex",
            flex: "1 1 0",
            flexDirection: "column",
            minHeight: 0,
            minWidth: 0,
          }}
        >
          {renderCardsView({
            items: filtered,
            expanded: expanded ?? null,
            onExpand: handleCardExpand,
            renderDetailPanel: (item) =>
              renderDetailPanel(item, () => handleCardExpand(null)),
          })}
        </div>
      </div>
    );
  }

  return (
    <>
      {!selectedItem && searchControls}

      <motion.div
        layout="position"
        transition={{
          duration: slowTransitionDurationSeconds,
          ease: EASING_STANDARD,
        }}
      >
        <AppGrid alignItems="flex-start" container spacing={3}>
          <AppGrid size={{ xs: 12, md: selectedItem ? 7 : 12 }}>
            {renderTableView({
              items: filtered,
              selected: expanded ?? null,
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
      </motion.div>
    </>
  );
}

export default UnitListTab;
