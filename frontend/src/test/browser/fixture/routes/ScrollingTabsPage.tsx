import type { CSSProperties } from "react";

import AppVirtualGrid from "@/components/grid/AppVirtualGrid";
import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

const items = Array.from({ length: 60 }, (_, index) => ({
  id: `item-${index}`,
}));

/**
 * MainLayout's header + scrollport, at the values it lays out with
 * (`theme.spacing(5)` is 20px). The tab strip is sticky against the scrollport,
 * so nothing about that contract is observable without a real one around it —
 * jsdom has no layout, and the fixture's other routes have no scrollport.
 *
 * `panel="grow"` is a panel taller than the scrollport (a card grid — those are
 * not virtualized), which is what scrolls the page. `panel="fill"` is a panel
 * that scrolls inside itself and must keep the page from scrolling at all.
 */
export default function ScrollingTabsPage({
  panel,
}: {
  panel: "fill" | "grow";
}) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100dvh",
        overflow: "hidden",
      }}
    >
      <div
        data-testid="fixture-header"
        style={{
          flexShrink: 0,
          minHeight: 64,
          background: "var(--app-palette-background-paper)",
        }}
      />
      <div
        data-testid="fixture-scrollport"
        style={{
          flex: 1,
          minHeight: 0,
          overflow: "auto",
          position: "relative",
        }}
      >
        <div
          style={
            {
              width: "100%",
              height: "100%",
              minHeight: "100%",
              padding: "20px",
              "--page-inset-block-start": "20px",
            } as CSSProperties
          }
        >
          <RoutedTabLayout tabs={tabs}>
            {panel === "fill" ? (
              <AppVirtualGrid
                estimateItemHeight={140}
                fillAvailable
                getItemKey={(item) => item.id}
                items={items}
                renderItem={(item) => (
                  <div style={{ height: 140 }}>{item.id}</div>
                )}
              />
            ) : (
              <div>
                {items.map((item) => (
                  <div key={item.id} style={{ height: 140 }}>
                    {item.id}
                  </div>
                ))}
              </div>
            )}
          </RoutedTabLayout>
        </div>
      </div>
    </div>
  );
}
