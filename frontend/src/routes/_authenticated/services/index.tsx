import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import ServicesTab from "./-components/ServicesTab";
import { SERVICES_TABS } from "./-components/servicesTabs";
import UnitViewToggle from "./-components/UnitViewToggle";

export const Route = createFileRoute("/_authenticated/services/")({
  validateSearch: (search) => ({
    ...optionalString(search, "service"),
  }),
  loaderDeps: ({ search }) => ({ service: search.service }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] = [
      linuxio.systemd.list_services.queryOptions(),
    ];
    if (deps.service) {
      queries.push(linuxio.systemd.get_unit_info.queryOptions(deps.service));
    }
    return loadRouteQueries({ context, preload }, queries);
  },
  component: ServicesRoute,
});

function ServicesRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const setSelected = useCallback(
    (service: string | null) =>
      navigate({
        search: (previous) => ({
          ...previous,
          service: service ?? undefined,
        }),
        to: "/services",
      }),
    [navigate],
  );

  return (
    <RoutedTabContainer
      containerStyle={{ paddingInline: 0 }}
      rightContent={<UnitViewToggle viewModeKey="services.list" />}
      tabs={SERVICES_TABS}
    >
      <ServicesTab onSelectedChange={setSelected} selected={search.service} />
    </RoutedTabContainer>
  );
}
