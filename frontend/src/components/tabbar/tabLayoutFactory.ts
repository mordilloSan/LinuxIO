import { Outlet } from "@tanstack/react-router";
import { createElement } from "react";

import type { RoutedTabEntry } from "./RoutedTabContainer";
import { RoutedTabLayout } from "./RoutedTabContainer";

export function makeTabLayout(tabs: readonly RoutedTabEntry[]) {
  return function RoutedTabRouteLayout() {
    return createElement(RoutedTabLayout, { tabs }, createElement(Outlet));
  };
}
