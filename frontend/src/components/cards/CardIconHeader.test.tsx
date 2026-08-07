import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import CardIconHeader from "./CardIconHeader";

const mocks = vi.hoisted(() => ({ titleTypographyRender: vi.fn() }));

vi.mock("@/components/ui/AppTypography", () => ({
  default: ({
    children,
    variant,
  }: {
    children?: ReactNode;
    variant?: string;
  }) => {
    if (variant === "subtitle1") {
      mocks.titleTypographyRender(children);
    }
    return <span>{children}</span>;
  },
}));

describe("CardIconHeader", () => {
  it("keeps its title stable while live header content changes", () => {
    const { rerender } = render(
      <CardIconHeader
        icon={<span>Icon</span>}
        right={<span>Right 1</span>}
        subtitle="Subtitle 1"
        title="Hardware"
      />,
    );

    expect(mocks.titleTypographyRender).toHaveBeenCalledTimes(1);

    rerender(
      <CardIconHeader
        icon={<span>Icon</span>}
        right={<span>Right 2</span>}
        subtitle="Subtitle 2"
        title="Hardware"
      />,
    );

    expect(screen.getByText("Right 2")).toBeInTheDocument();
    expect(screen.getByText("Subtitle 2")).toBeInTheDocument();
    expect(mocks.titleTypographyRender).toHaveBeenCalledTimes(1);

    rerender(
      <CardIconHeader
        icon={<span>Icon</span>}
        right={<span>Right 2</span>}
        subtitle="Subtitle 2"
        title="Monitoring"
      />,
    );

    expect(screen.getByText("Monitoring")).toBeInTheDocument();
    expect(mocks.titleTypographyRender).toHaveBeenCalledTimes(2);
  });
});
