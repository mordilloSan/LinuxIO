import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import DockItem from "./DockItem";

interface CapturedLinkProps {
  "aria-describedby"?: unknown;
  children: ReactNode;
  disabled?: unknown;
  params?: unknown;
  preload?: unknown;
  preloadDelay?: unknown;
  tabIndex?: unknown;
  to?: unknown;
}

const linkProps = vi.hoisted(() => ({
  calls: [] as CapturedLinkProps[],
}));

const sortable = vi.hoisted(() => ({ onKeyDown: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  Link: (props: CapturedLinkProps) => {
    linkProps.calls.push(props);
    return <a>{props.children}</a>;
  },
}));

vi.mock("@dnd-kit/sortable", () => ({
  useSortable: () => ({
    attributes: { "aria-describedby": "dnd-instructions" },
    isDragging: false,
    listeners: { onKeyDown: sortable.onKeyDown },
    setNodeRef: () => {},
    transform: null,
    transition: undefined,
  }),
}));

vi.mock("./DockTile", () => ({
  default: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

describe("DockItem", () => {
  beforeEach(() => {
    linkProps.calls.length = 0;
    sortable.onKeyDown.mockReset();
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

  it("drops its href in layout mode but stays focusable", () => {
    render(
      <DockItem editMode gradient={["#fff", "#000"]} title="Logs" to="/logs" />,
    );

    expect(linkProps.calls.at(-1)).toMatchObject({
      "aria-describedby": "dnd-instructions",
      disabled: true,
      tabIndex: 0,
    });
  });

  it("starts a keyboard drag on Space but leaves Enter to the link", () => {
    render(<DockItem gradient={["#fff", "#000"]} title="Logs" to="/logs" />);
    const item = screen.getByRole("listitem");

    fireEvent.keyDown(item, { key: "Enter" });
    expect(sortable.onKeyDown).not.toHaveBeenCalled();

    fireEvent.keyDown(item, { key: " " });
    expect(sortable.onKeyDown).toHaveBeenCalledTimes(1);
  });
});
