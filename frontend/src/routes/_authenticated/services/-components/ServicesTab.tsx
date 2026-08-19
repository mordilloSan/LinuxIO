import { useSuspenseQuery } from "@tanstack/react-query";

import type { linuxio } from "@/api";
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

function useServicesQuery(
  listQueryOptions: typeof linuxio.systemd.list_services,
  viewMode: TableCardViewMode,
) {
  return useSuspenseQuery({
    ...listQueryOptions,
    refetchInterval: viewMode === "card" ? false : 2000,
  });
}

interface ServicesTabProps {
  listQueryOptions: typeof linuxio.systemd.list_services;
  onSelectedChange: (name: string | null) => void;
  selected?: string;
  selectedQueryOptions:
    | ReturnType<typeof linuxio.systemd.get_unit_info>
    | undefined;
  viewMode: TableCardViewMode;
}

const ServicesTab = ({
  listQueryOptions,
  onSelectedChange,
  selected,
  selectedQueryOptions,
  viewMode,
}: ServicesTabProps) => {
  const { data } = useServicesQuery(listQueryOptions, viewMode);

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
        <UnitInfoPanel
          onClose={onClose}
          queryOptions={
            selected === service.name ? selectedQueryOptions : undefined
          }
          unitName={service.name}
        />
      )}
      renderTableView={({ items, onSelect, surface }) => (
        <ServiceTableView
          onSelect={onSelect}
          services={items}
          surface={surface}
        />
      )}
      searchPlaceholder="Search services…"
      selected={selected}
      surfaceId="services.list"
      viewMode={viewMode}
    />
  );
};

export default ServicesTab;
