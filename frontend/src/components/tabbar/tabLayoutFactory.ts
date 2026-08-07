import { Outlet } from "@tanstack/react-router";
import { createElement, type CSSProperties } from "react";

import type { RoutedTab } from "./RoutedTabContainer";
import { RoutedTabLayout } from "./RoutedTabContainer";

export function makeTabLayout(
  tabs: readonly RoutedTab[],
  containerStyle?: CSSProperties,
) {
  return function RoutedTabRouteLayout() {
    return createElement(
      RoutedTabLayout,
      { containerStyle, tabs },
      createElement(Outlet),
    );
  };
}
