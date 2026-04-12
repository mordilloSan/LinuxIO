import React from "react";

import ServiceCardsView from "./ServiceCardsView";
import ServiceTableView from "./ServiceTableView";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel } from "./UnitViews";

import type { Service } from "@/api";
import { linuxio } from "@/api";
import { useViewMode } from "@/hooks/useViewMode";
import type { TableCardViewMode } from "@/types/config";

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
  return linuxio.dbus.list_services.useQuery({
    refetchInterval: viewMode === "card" ? false : 2000,
  });
}

const ServicesTab: React.FC = () => {
  const [viewMode, setViewMode] = useViewMode("services.list", "table");
  const { data, isPending, isError, error } = useServicesQuery(viewMode);

  return (
    <UnitListTab
      viewMode={viewMode}
      setViewMode={setViewMode}
      data={data}
      isPending={isPending}
      isError={isError}
      error={error}
      searchPlaceholder="Search services…"
      errorMessage="Failed to load services"
      compareItems={compareServicesByName}
      matchesSearch={matchesServiceSearch}
      urlParam="service"
      renderTableView={({ items, selected, onSelect, onDoubleClick }) => (
        <ServiceTableView
          services={items}
          selected={selected}
          onSelect={onSelect}
          onDoubleClick={onDoubleClick}
        />
      )}
      renderCardsView={({ items, expanded, onExpand, renderDetailPanel }) => (
        <ServiceCardsView
          services={items}
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
        />
      )}
      renderDetailPanel={(service, onClose) => (
        <UnitInfoPanel unitName={service.name} onClose={onClose} />
      )}
    />
  );
};

export default ServicesTab;
