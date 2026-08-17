import {
  createElement,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { act, renderHook } from "@/test/render";

import {
  calculateDockRenderScale,
  calculateDockTargets,
  DOCK_POINTER_ATTRIBUTE,
  DOCK_TILE_SIZE,
  DOCK_TILE_SIZE_MAX,
  DockMagnificationProvider,
  useDockPointerLiveness,
} from "./useDockMagnification";

const wrapper = ({ children }: { children: ReactNode }) =>
  createElement(DockMagnificationProvider, null, children);

const pointer = (pointerType: "mouse" | "touch", clientX = 100) =>
  ({ pointerType, clientX }) as unknown as ReactPointerEvent<HTMLElement>;

function setupLiveness(enabled = true) {
  const hook = renderHook(
    ({ magnificationEnabled }: { magnificationEnabled: boolean }) =>
      useDockPointerLiveness(magnificationEnabled),
    { initialProps: { magnificationEnabled: enabled }, wrapper },
  );
  const nav = document.createElement("nav");
  document.body.append(nav);
  hook.result.current.navRef.current = nav;
  return { ...hook, nav };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("calculateDockRenderScale", () => {
  it("renders the visible range from the maximum raster size", () => {
    const restScale = calculateDockRenderScale(1);
    const peakScale = calculateDockRenderScale(
      DOCK_TILE_SIZE_MAX / DOCK_TILE_SIZE,
    );

    expect(restScale).toBe(0.625);
    expect(restScale * DOCK_TILE_SIZE_MAX).toBe(DOCK_TILE_SIZE);
    expect(peakScale).toBe(1);
  });
});

describe("calculateDockTargets", () => {
  it("returns rest targets out of range", () => {
    expect(calculateDockTargets([100, 150], Number.POSITIVE_INFINITY)).toEqual([
      { scale: 1, x: 0 },
      { scale: 1, x: 0 },
    ]);
  });

  it("centers cumulative expansion around the original dock", () => {
    const targets = calculateDockTargets([100, 140, 180], 140);
    expect(targets[1].scale).toBe(1.6);
    expect(targets[0].x + targets[2].x).toBeCloseTo(0);
  });

  it("reconstructs the same centers as cumulative flex expansion", () => {
    const targets = calculateDockTargets([20, 66, 112, 158], 66);
    const expansions = targets.map(({ scale }) => (scale - 1) * 40);
    const totalExpansion = expansions.reduce((sum, value) => sum + value, 0);
    let precedingExpansion = 0;

    targets.forEach(({ x }, index) => {
      expect(x).toBeCloseTo(
        precedingExpansion + expansions[index] / 2 - totalExpansion / 2,
      );
      precedingExpansion += expansions[index];
    });
  });
});

describe("useDockPointerLiveness", () => {
  it("arms on mouse activity and clears for either touch path", () => {
    const { result, nav, unmount } = setupLiveness();

    act(() => result.current.onPointerDown(pointer("mouse")));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");

    act(() => result.current.onPointerDown(pointer("touch")));
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);

    act(() => result.current.onPointerMove(pointer("mouse")));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");

    act(() => result.current.onPointerMove(pointer("touch")));
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);

    unmount();
    nav.remove();
  });

  it("resets liveness on pointerleave", () => {
    const { result, nav, unmount } = setupLiveness();

    act(() => result.current.onPointerMove(pointer("mouse")));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");

    act(() => result.current.onPointerLeave());
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);
    unmount();
    nav.remove();
  });

  it("resets liveness on window blur", () => {
    const { result, nav, unmount } = setupLiveness();

    act(() => result.current.onPointerMove(pointer("mouse")));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");

    act(() => {
      window.dispatchEvent(new Event("blur"));
    });
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);

    unmount();
    nav.remove();
  });

  it("resets liveness when the document is hidden", () => {
    const { result, nav, unmount } = setupLiveness();

    act(() => result.current.onPointerMove(pointer("mouse")));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");

    const visibility = vi
      .spyOn(document, "visibilityState", "get")
      .mockReturnValue("hidden");
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);

    visibility.mockRestore();
    unmount();
    nav.remove();
  });

  it("resets liveness across either magnification breakpoint transition", () => {
    const frames: FrameRequestCallback[] = [];
    const requestFrame = vi
      .spyOn(window, "requestAnimationFrame")
      .mockImplementation((callback) => {
        frames.push(callback);
        return frames.length;
      });
    const { result, nav, rerender, unmount } = setupLiveness();

    act(() => result.current.onPointerMove(pointer("mouse", 120)));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");
    expect(requestFrame).toHaveBeenCalledTimes(1);
    act(() => frames.splice(0).forEach((frame) => frame(0)));

    rerender({ magnificationEnabled: false });
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);
    expect(requestFrame).toHaveBeenCalledTimes(2);
    act(() => frames.splice(0).forEach((frame) => frame(0)));

    // Pointer liveness remains useful for labels while magnification is off,
    // but it must not feed another magnification frame.
    act(() => result.current.onPointerMove(pointer("mouse", 140)));
    expect(nav).toHaveAttribute(DOCK_POINTER_ATTRIBUTE, "");
    expect(requestFrame).toHaveBeenCalledTimes(2);

    rerender({ magnificationEnabled: true });
    expect(nav).not.toHaveAttribute(DOCK_POINTER_ATTRIBUTE);
    expect(requestFrame).toHaveBeenCalledTimes(3);
    act(() => frames.splice(0).forEach((frame) => frame(0)));

    unmount();
    nav.remove();
  });
});
