import React from "react";

import ServiceCardsView from "./ServiceCardsView";
import ServiceTableView from "./ServiceTableView";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel } from "./UnitViews";

import { linuxio } from "@/api";
import type { Service, TableCardViewMode } from "@/api";
import { useViewMode } from "@/hooks/useViewMode";

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
  return linuxio.systemd.list_services.useQuery({
    refetchInterval: viewMode === "card" ? false : 2000,
  });
}

const ServicesTab: React.FC = () => {
  const [viewMode, setViewMode] = useViewMode("services.list", "table");
  const { data, isPending, isError, error } = useServicesQuery(viewMode);

  return (
    <UnitListTab
      compareItems={compareServicesByName}
      data={data}
      error={error}
      errorMessage="Failed to load services"
      isError={isError}
      isPending={isPending}
      matchesSearch={matchesServiceSearch}
      renderCardsView={({ items, expanded, onExpand, renderDetailPanel }) => (
        <ServiceCardsView
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
          services={items}
        />
      )}
      renderDetailPanel={(service, onClose) => (
        <UnitInfoPanel onClose={onClose} unitName={service.name} />
      )}
      renderTableView={({ items, selected, onSelect, onDoubleClick }) => (
        <ServiceTableView
          onDoubleClick={onDoubleClick}
          onSelect={onSelect}
          selected={selected}
          services={items}
        />
      )}
      searchPlaceholder="Search services…"
      setViewMode={setViewMode}
      urlParam="service"
      viewMode={viewMode}
    />
  );
};

export default ServicesTab;
