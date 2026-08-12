import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import type { Service, TableCardViewMode } from "@/api";

import ServiceCardsView from "./ServiceCardsView";
import ServiceTableView from "./ServiceTableView";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel } from "./UnitViews";

function compareServicesByName(a: Service, b: Service): number {
  return a.name.localeCompare(b.name, undefined, {
    numeric: true,
    sensitivity: "base",
  });
}

function matchesServiceSearch(service: Service, search: string): boolean {
  return (
    service.name.toLowerCase().includes(search) ||
    (service.description?.toLowerCase().includes(search) ?? false)
  );
}

function useServicesQuery(viewMode: TableCardViewMode) {
  return useSuspenseQuery({
    ...linuxio.systemd.list_services,
    refetchInterval: viewMode === "card" ? false : 2000,
  });
}

interface ServicesTabProps {
  onSelectedChange: (name: string | null) => void;
  onViewModeChange: (next: TableCardViewMode) => void;
  selected?: string;
  viewMode: TableCardViewMode;
}

const ServicesTab = ({
  onSelectedChange,
  onViewModeChange,
  selected,
  viewMode,
}: ServicesTabProps) => {
  const { data } = useServicesQuery(viewMode);

  return (
    <UnitListTab
      compareItems={compareServicesByName}
      data={data}
      matchesSearch={matchesServiceSearch}
      onSelectedChange={onSelectedChange}
      renderCardsView={({
        items,
        expanded,
        onExpand,
        renderDetailPanel,
        surface,
      }) => (
        <ServiceCardsView
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
          services={items}
          surface={surface}
        />
      )}
      renderDetailPanel={(service, onClose) => (
        <UnitInfoPanel onClose={onClose} unitName={service.name} />
      )}
      renderTableView={({
        items,
        selected,
        onSelect,
        onDoubleClick,
        surface,
      }) => (
        <ServiceTableView
          onDoubleClick={onDoubleClick}
          onSelect={onSelect}
          selected={selected}
          services={items}
          surface={surface}
        />
      )}
      searchPlaceholder="Search services…"
      selected={selected}
      setViewMode={onViewModeChange}
      surfaceId="services.list"
      viewMode={viewMode}
    />
  );
};

export default ServicesTab;
