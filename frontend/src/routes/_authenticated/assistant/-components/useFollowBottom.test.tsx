import { act, render } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";

import { useFollowBottom } from "./useFollowBottom";

function Harness({ session }: { session?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useFollowBottom(ref, session);
  return <div data-testid="box" ref={ref} />;
}

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
  box.scrollTop = 800;
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
});
