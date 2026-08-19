import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import DockItem from "./DockItem";

interface CapturedLinkProps {
  children: ReactNode;
  params?: unknown;
  preload?: unknown;
  preloadDelay?: unknown;
  to?: unknown;
}

const linkProps = vi.hoisted(() => ({
  calls: [] as CapturedLinkProps[],
}));

vi.mock("@tanstack/react-router", () => ({
  Link: (props: CapturedLinkProps) => {
    linkProps.calls.push(props);
    return <a>{props.children}</a>;
  },
}));

vi.mock("./DockTile", () => ({
  default: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

describe("DockItem", () => {
  beforeEach(() => {
    linkProps.calls.length = 0;
  });

  it("inherits global intent preloading without per-link overrides", () => {
    render(
      <DockItem
        gradient={["#fff", "#000"]}
        params={{ _splat: "" }}
        title="Navigator"
        to="/filebrowser/$"
      />,
    );

    expect(linkProps.calls).toHaveLength(1);
    expect(linkProps.calls[0]).toMatchObject({
      params: { _splat: "" },
      to: "/filebrowser/$",
    });
    expect("preload" in linkProps.calls[0]).toBe(false);
    expect("preloadDelay" in linkProps.calls[0]).toBe(false);
  });
});
