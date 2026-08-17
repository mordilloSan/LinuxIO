import { motion } from "motion/react";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import type { TableCardViewMode } from "@/api";
import { RoutedTabSearch } from "@/components/tabbar";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import {
  useReorderableSurface,
  type ReorderableSurface,
} from "@/hooks/useReorderableSurface";
import {
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";

import type { UnitListItem } from "./UnitViews";

interface UnitTableViewRenderProps<T> {
  items: T[];
  onSelect: (name: string | null) => void;
  surface: ReorderableSurface<T>;
}

interface UnitCardsViewRenderProps<T> {
  expanded: string | null;
  items: T[];
  onExpand: (name: string | null) => void;
  renderDetailPanel: (item: T) => ReactNode;
  surface: ReorderableSurface<T>;
}

const getUnitId = (unit: UnitListItem) => unit.name;

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
  /** Surface id the manual order is stored under, e.g. "services.list". */
  surfaceId: string;
  viewMode: TableCardViewMode;
}

function UnitListTab<T extends UnitListItem>({
  viewMode,
  data,
  searchPlaceholder,
  compareItems,
  matchesSearch,
  renderTableView,
  renderCardsView,
  renderDetailPanel,
  selected,
  onSelectedChange,
  surfaceId,
}: UnitListTabProps<T>) {
  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const [search, setSearch] = useState("");
  const expanded = selected;
  const setExpanded = useCallback(
    (name: string | null) => onSelectedChange(name),
    [onSelectedChange],
  );
  const handleEscapeKey = useEffectEvent((event: KeyboardEvent) => {
    if (event.key !== "Escape") {
      return;
    }

    setExpanded(null);
  });

  useEffect(() => {
    window.addEventListener("keydown", handleEscapeKey);
    return () => window.removeEventListener("keydown", handleEscapeKey);
  }, []);

  // The alphabetical sort is only the starting order: a saved manual order wins
  // over it, and anything the user never moved stays alphabetical.
  const sorted = useMemo(
    () => [...data].sort(compareItems),
    [compareItems, data],
  );
  const surface = useReorderableSurface({
    getId: getUnitId,
    items: sorted,
    surface: surfaceId,
  });
  const orderedItems = surface.items;
  const filtered = useMemo(() => {
    const searchText = search.trim().toLowerCase();

    return orderedItems.filter((item) => matchesSearch(item, searchText));
  }, [matchesSearch, orderedItems, search]);

  const selectedItem = expanded
    ? (filtered.find((item) => item.name === expanded) ?? null)
    : null;

  const searchControl = (
    <RoutedTabSearch active={search !== ""}>
      <AppHeaderSearch
        onChange={setSearch}
        placeholder={searchPlaceholder}
        value={search}
      />
    </RoutedTabSearch>
  );

  // Selecting a unit replaces whichever list you came from with the same detail
  // layout, so the table gets the card view's detail for free — the same model
  // as the Docker container list. `viewMode` is left alone, so clearing the
  // selection drops you back into the list you were already in.
  if (viewMode === "card" || selectedItem) {
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
        {!selectedItem && searchControl}
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
            onExpand: setExpanded,
            renderDetailPanel: (item) =>
              renderDetailPanel(item, () => setExpanded(null)),
            surface,
          })}
        </div>
      </div>
    );
  }

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
      {searchControl}

      <motion.div
        layout="position"
        style={{
          display: "flex",
          flex: "1 1 0",
          flexDirection: "column",
          minHeight: 0,
          minWidth: 0,
        }}
        transition={{
          duration: slowTransitionDurationSeconds,
          ease: EASING_STANDARD,
        }}
      >
        {renderTableView({
          items: filtered,
          onSelect: setExpanded,
          surface,
        })}
      </motion.div>
    </div>
  );
}

export default UnitListTab;
