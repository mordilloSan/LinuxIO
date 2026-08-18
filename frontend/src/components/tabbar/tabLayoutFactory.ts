import { Outlet } from "@tanstack/react-router";
import { createElement } from "react";

import type { RoutedTab } from "./RoutedTabContainer";
import { RoutedTabLayout } from "./RoutedTabContainer";

export function makeTabLayout(tabs: readonly RoutedTab[]) {
  return function RoutedTabRouteLayout() {
    return createElement(RoutedTabLayout, { tabs }, createElement(Outlet));
  };
}
