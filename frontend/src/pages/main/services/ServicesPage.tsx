import React from "react";

import ServicesTab from "./ServicesTab";
import SocketsTab from "./SocketsTab";
import TimersTab from "./TimersTab";
import UnitViewToggle from "./UnitViewToggle";

import TabContainer from "@/components/tabbar/TabContainer";

const TABS = [
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

const ServicesPage: React.FC = () => (
  <TabContainer
    tabs={TABS}
    defaultTab="services"
    urlParam="section"
    containerStyle={{ paddingInline: 0 }}
  />
);

export default ServicesPage;
