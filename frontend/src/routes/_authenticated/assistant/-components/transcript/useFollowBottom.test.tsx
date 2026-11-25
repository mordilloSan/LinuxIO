import { act, render } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useFollowBottom } from "./useFollowBottom";

function Harness({ session }: { session?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useFollowBottom(ref, session);
  return <div data-testid="box" ref={ref} />;
}

// The hook's ResizeObserver, so a test can report a size change.
let resized: () => void = () => {};
class ResizeObserverStub {
  constructor(callback: () => void) {
    resized = callback;
  }
  observe() {}
  disconnect() {}
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const grow = (box: HTMLElement, height: number) =>
  Object.defineProperty(box, "scrollHeight", {
    value: height,
    configurable: true,
  });

const setup = () => {
  const view = render(<Harness session="a" />);
  const box = view.getByTestId("box");
  Object.defineProperty(box, "scrollHeight", {
    value: 1000,
    configurable: true,
  });
  Object.defineProperty(box, "clientHeight", {
    value: 200,
    configurable: true,
  });
  // At the bottom, with the scroll event a browser sends for it.
  box.scrollTop = 800;
  act(() => {
    box.dispatchEvent(new Event("scroll"));
  });
  return { ...view, box };
};

describe("useFollowBottom", () => {
  it("follows new content while at the bottom", () => {
    const { box, rerender } = setup();
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    Object.defineProperty(box, "scrollHeight", {
      value: 1200,
      configurable: true,
    });
    rerender(<Harness session="a" />);
    expect(box.scrollTop).toBe(1200);
  });

  it("stays put after the user scrolls up", () => {
    const { box, rerender } = setup();
    box.scrollTop = 100;
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    Object.defineProperty(box, "scrollHeight", {
      value: 1200,
      configurable: true,
    });
    rerender(<Harness session="a" />);
    expect(box.scrollTop).toBe(100);
  });

  it("resumes following when the session changes", () => {
    const { box, rerender } = setup();
    box.scrollTop = 100;
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    Object.defineProperty(box, "scrollHeight", {
      value: 1200,
      configurable: true,
    });
    rerender(<Harness session="b" />);
    expect(box.scrollTop).toBe(1200);
  });

  it("keeps following when content grows before its own scroll lands", () => {
    const { box, rerender } = setup();
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    // The pinned position, read after the content grew past it.
    grow(box, 1200);
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    rerender(<Harness session="a" />);
    expect(box.scrollTop).toBe(1200);
  });

  it("follows a size change without a render", () => {
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    const { box } = setup();
    grow(box, 1200);
    resized();
    expect(box.scrollTop).toBe(1200);
  });

  it("stops following a size change after a scroll up, until back at the bottom", () => {
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    const { box } = setup();
    box.scrollTop = 700;
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    grow(box, 1200);
    resized();
    expect(box.scrollTop).toBe(700);

    box.scrollTop = 1000;
    act(() => {
      box.dispatchEvent(new Event("scroll"));
    });
    grow(box, 1400);
    resized();
    expect(box.scrollTop).toBe(1400);
  });
});
