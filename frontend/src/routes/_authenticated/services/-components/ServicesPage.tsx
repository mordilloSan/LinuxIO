import { getRouteApi } from "@tanstack/react-router";

import TabContainer from "@/components/tabbar/TabContainer";

import ServicesTab from "./ServicesTab";
import SocketsTab from "./SocketsTab";
import TimersTab from "./TimersTab";
import UnitViewToggle from "./UnitViewToggle";

const servicesRouteApi = getRouteApi("/_authenticated/services");

const ServicesPage = () => {
  const navigate = servicesRouteApi.useNavigate();
  const search = servicesRouteApi.useSearch();
  const activeTab =
    search.section === "timers" || search.section === "sockets"
      ? search.section
      : "services";
  const tabs = [
    {
      value: "services",
      label: "Services",
      component: <ServicesTab />,
      rightContent: <UnitViewToggle viewModeKey="services.list" />,
    },
    {
      value: "timers",
      label: "Timers",
      component: <TimersTab />,
      rightContent: <UnitViewToggle viewModeKey="timers.list" />,
    },
    {
      value: "sockets",
      label: "Sockets",
      component: <SocketsTab />,
      rightContent: <UnitViewToggle viewModeKey="sockets.list" />,
    },
  ];

  return (
    <TabContainer
      activeTab={activeTab}
      containerStyle={{ paddingInline: 0 }}
      onTabChange={(section) =>
        navigate({
          to: "/services",
          search: (previous) => ({ ...previous, section }),
        })
      }
      tabs={tabs}
    />
  );
};

export default ServicesPage;
