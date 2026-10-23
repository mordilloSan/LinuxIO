import {
  useSuspenseQuery,
  type UseSuspenseQueryOptions,
} from "@tanstack/react-query";

import {
  CACHE_TTL_MS,
  type linuxio,
  type LinuxIOError,
  type TableCardViewMode,
} from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import type { ReorderableSurface } from "@/components/reorder/useReorderableSurface";
import { RoutedTabActions } from "@/components/tabbar";
import { useReorderableTableDnd } from "@/components/tables/useReorderableTableDnd";
import AppTypography from "@/components/ui/AppTypography";
import { DetailRow } from "@/components/ui/InfoRow";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import UnitInfoPanel from "./UnitInfoPanel";
import {
  UNIT_MOBILE_COLUMNS,
  type UnitCardRow,
  type UnitKind,
} from "./unitKinds";
import UnitListTab from "./UnitListTab";
import {
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
  UnitTableView,
  type UnitListItem,
} from "./UnitViews";

/** One refresh rate for every unit list. Actions invalidate it; this covers outside changes. */
const UNIT_LIST_REFETCH_MS = CACHE_TTL_MS.FIVE_SECONDS;

const getUnitId = (unit: UnitListItem) => unit.name;

const compareUnitsByName = (a: UnitListItem, b: UnitListItem) =>
  a.name.localeCompare(b.name, undefined, {
    numeric: true,
    sensitivity: "base",
  });

const UnitCardRows = ({ rows }: { rows: UnitCardRow[] }) =>
  rows
    .filter((row) => !row.hidden)
    .map((row) => (
      <DetailRow key={row.label} label={row.label}>
        <AppTypography
          color={row.color}
          component="span"
          fontWeight={500}
          variant="caption"
        >
          {row.value}
        </AppTypography>
      </DetailRow>
    ));

interface UnitKindTabProps<T extends UnitListItem> {
  kind: UnitKind<T>;
  listQueryOptions: UseSuspenseQueryOptions<T[], LinuxIOError, T[]>;
  onSelectedChange: (name: string | null) => void;
  selected?: string;
  selectedQueryOptions:
    | ReturnType<typeof linuxio.systemd.get_unit_info>
    | undefined;
  viewMode: TableCardViewMode;
}

function UnitKindTab<T extends UnitListItem>({
  kind,
  listQueryOptions,
  onSelectedChange,
  selected,
  selectedQueryOptions,
  viewMode,
}: UnitKindTabProps<T>) {
  const { data } = useSuspenseQuery({
    ...listQueryOptions,
    refetchInterval: viewMode === "card" ? false : UNIT_LIST_REFETCH_MS,
  });
  const Noun = kind.noun.charAt(0).toUpperCase() + kind.noun.slice(1);

  return (
    <UnitListTab
      compareItems={compareUnitsByName}
      data={data}
      matchesSearch={kind.matchesSearch}
      onSelectedChange={onSelectedChange}
      renderCardsView={({
        items,
        expanded,
        onExpand,
        renderDetailPanel,
        surface,
      }) => (
        <UnitCardsView
          emptyMessage={`No ${kind.noun}s found.`}
          expanded={expanded}
          items={items}
          onExpand={onExpand}
          renderActions={(item, info) => (
            <UnitCardActions
              activeState={item.active_state}
              info={info}
              unitFileState={item.unit_file_state}
              unitName={item.name}
            />
          )}
          renderBottomPanel={(item) => (
            <UnitLogsCard title={`${Noun} Logs`} unitName={item.name} />
          )}
          renderDetailPanel={renderDetailPanel}
          renderSelectedRows={(item, info) => (
            <UnitCardRows rows={kind.cardRows(item, info)} />
          )}
          renderSummaryRows={(item) => (
            <UnitStatusRows
              activeEnterTimestamp={item.active_enter_timestamp}
              activeLabel={kind.activeLabel}
              activeState={item.active_state}
              inactiveEnterTimestamp={item.inactive_enter_timestamp}
              subState={item.sub_state}
              unitFileState={item.unit_file_state}
            />
          )}
          surface={surface}
        />
      )}
      renderDetailPanel={(item) => (
        <UnitInfoPanel
          queryOptions={
            selected === item.name ? selectedQueryOptions : undefined
          }
          renderInfoRows={
            kind.panelRows && ((info) => kind.panelRows?.(item, info) ?? [])
          }
          unitName={item.name}
        />
      )}
      renderTableView={({ items, onSelect, surface }) => (
        <UnitKindTable
          items={items}
          kind={kind}
          onSelect={onSelect}
          surface={surface}
        />
      )}
      searchPlaceholder={`Search ${kind.noun}s…`}
      selected={selected}
      surfaceId={kind.surfaceId}
      viewMode={viewMode}
    />
  );
}

interface UnitKindTableProps<T extends UnitListItem> {
  items: T[];
  kind: UnitKind<T>;
  onSelect: (name: string | null) => void;
  surface: ReorderableSurface<T>;
}

function UnitKindTable<T extends UnitListItem>({
  items,
  kind,
  onSelect,
  surface,
}: UnitKindTableProps<T>) {
  const dnd = useReorderableTableDnd<T, T>({
    handleAriaLabel: `Reorder ${kind.noun}`,
    surface,
  });

  return (
    <UnitTableView
      data={items}
      desktopColumns={kind.desktopColumns}
      dnd={dnd}
      emptyMessage={`No ${kind.noun}s found.`}
      getRowKey={getUnitId}
      mobileColumns={UNIT_MOBILE_COLUMNS}
      onSelect={(key) => onSelect(typeof key === "string" ? key : null)}
      renderMainRow={kind.renderTableRow}
    />
  );
}

type UnitKindPageProps<T extends UnitListItem> = Omit<
  UnitKindTabProps<T>,
  "viewMode"
>;

/** Body of a services, timers or sockets route: view toggle plus the unit list. */
function UnitKindPage<T extends UnitListItem>(props: UnitKindPageProps<T>) {
  const [viewMode, setViewMode] = useViewMode(props.kind.surfaceId);

  return (
    <>
      <RoutedTabActions>
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setViewMode}
          viewMode={viewMode}
        />
      </RoutedTabActions>
      <UnitKindTab {...props} viewMode={viewMode} />
    </>
  );
}

export default UnitKindPage;
