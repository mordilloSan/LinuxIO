import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import type {
  ReorderableSurface,
  ReorderableSurfaceDndProps,
} from "@/hooks/useReorderableSurface";
import { render } from "@/test/render";
import { DASHBOARD_CARD_GAP } from "@/theme/constants";

import ReorderableCardGrid from "./ReorderableCardGrid";

vi.mock("@/components/cards/SortableCard", () => ({
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/reorder/ReorderableArea", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

interface TestItem {
  id: string;
}

const items: TestItem[] = [{ id: "one" }];
const surface = {
  dndContextProps: {} as ReorderableSurfaceDndProps,
  editMode: false,
  exitEditMode: vi.fn(),
  ids: items.map((item) => item.id),
  items,
  pendingId: null,
} satisfies ReorderableSurface<TestItem>;

describe("ReorderableCardGrid", () => {
  it("uses the global dashboard card gap by default", () => {
    const { container } = render(
      <ReorderableCardGrid
        getId={(item) => item.id}
        renderItem={(item) => item.id}
        size={12}
        surface={surface}
      />,
    );

    expect(container.querySelector(".app-grid")).toHaveStyle({
      gap: `${DASHBOARD_CARD_GAP}px`,
    });
  });
});
