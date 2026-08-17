import type { CSSProperties, ReactNode } from "react";

import AppVirtualGrid from "@/components/grid/AppVirtualGrid";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import { ConfigContext } from "@/contexts/ConfigContext";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import type { ConfigContextType } from "@/types/config";

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

interface FixtureItem {
  id: string;
}

const items: FixtureItem[] = Array.from({ length: 60 }, (_, index) => ({
  id: `item-${index}`,
}));

const getItemId = (item: FixtureItem) => item.id;

// ReorderableCardGrid reads a saved order through useConfigValue. The fixture
// has no ConfigProvider and none of these tests touch reordering, so an empty
// order and a no-op writer are the whole surface it needs.
const stubConfig = {
  config: { appSettings: { layoutOrders: {} } },
  setKey: () => {},
} as unknown as ConfigContextType;

function StubConfigProvider({ children }: { children: ReactNode }) {
  return <ConfigContext value={stubConfig}>{children}</ConfigContext>;
}

function CardPanel() {
  const surface = useReorderableSurface({
    getId: getItemId,
    items,
    surface: "fixture.cards",
  });

  return (
    <ReorderableCardGrid
      fillAvailable
      getId={getItemId}
      renderItem={(item) => <div style={{ height: 140 }}>{item.id}</div>}
      size={CARD_GRID_SIZE_STANDARD}
      surface={surface}
    />
  );
}

/**
 * MainLayout's header + scrollport, at the values it lays out with
 * (`theme.spacing(5)` is 20px). The tab strip is sticky against the scrollport,
 * so nothing about that contract is observable without a real one around it —
 * jsdom has no layout, and the fixture's other routes have no scrollport.
 *
 * `panel="grow"` is a panel taller than the scrollport, which is what scrolls
 * the page. `panel="fill"` and `panel="cards"` own their overflow instead and
 * must keep the page from scrolling at all — the virtualized grid and the card
 * grid respectively, since those are the two scrollports routes actually use.
 */
export default function ScrollingTabsPage({
  panel,
}: {
  panel: "cards" | "fill" | "grow";
}) {
  return (
    <StubConfigProvider>
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
              {panel === "cards" ? (
                <CardPanel />
              ) : panel === "fill" ? (
                <AppVirtualGrid
                  estimateItemHeight={140}
                  fillAvailable
                  getItemKey={getItemId}
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
    </StubConfigProvider>
  );
}
