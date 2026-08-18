import { useState } from "react";

import AppVirtualGrid from "@/components/grid/AppVirtualGrid";
import AppButton from "@/components/ui/AppButton";

interface GridItem {
  id: string;
}

const initialItems: GridItem[] = Array.from({ length: 180 }, (_, index) => ({
  id: `grid-item-${index}`,
}));

export default function VirtualGridPage() {
  const [items, setItems] = useState(initialItems);
  const [wideCards, setWideCards] = useState(false);
  const [tallFirst, setTallFirst] = useState(false);

  return (
    <main
      style={{
        boxSizing: "border-box",
        height: "100dvh",
        padding: 24,
      }}
    >
      <h1>Virtual grid fixture</h1>
      <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
        <AppButton
          onClick={() =>
            setItems((current) => [
              { id: `grid-prepend-${current.length}` },
              ...current,
            ])
          }
        >
          Prepend item
        </AppButton>
        <AppButton
          onClick={() =>
            setItems((current) => [
              ...current,
              { id: `grid-append-${current.length}` },
            ])
          }
        >
          Append item
        </AppButton>
        <AppButton onClick={() => setTallFirst((value) => !value)}>
          Toggle first height
        </AppButton>
        <AppButton onClick={() => setWideCards((value) => !value)}>
          Toggle columns
        </AppButton>
      </div>
      <div data-testid="virtual-grid-status">
        items: {items.length}; mode: {wideCards ? "wide" : "standard"}; first:{" "}
        {tallFirst ? "tall" : "standard"}
      </div>
      <AppVirtualGrid
        ariaLabel="Virtual grid fixture"
        fillAvailable={false}
        getItemKey={(item) => item.id}
        height={420}
        items={items}
        minItemWidth={wideCards ? 420 : 180}
        renderItem={(item, index) => (
          <div
            data-testid={`grid-card-${index}`}
            style={{
              alignItems: "center",
              background: "var(--app-palette-background-paper)",
              border: "1px solid var(--app-palette-divider)",
              boxSizing: "border-box",
              display: "flex",
              height: index === 0 && tallFirst ? 160 : 56,
              justifyContent: "center",
              minWidth: 0,
            }}
          >
            {item.id}
          </div>
        )}
        overscan={12}
        padding={8}
        style={{ width: "100%" }}
      />
    </main>
  );
}
