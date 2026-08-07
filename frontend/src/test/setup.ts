import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

class ResizeObserverMock implements ResizeObserver {
  disconnect = vi.fn();
  observe = vi.fn();
  unobserve = vi.fn();
}

Object.defineProperty(window, "matchMedia", {
  configurable: true,
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    addEventListener: vi.fn(),
    addListener: vi.fn(),
    dispatchEvent: vi.fn(),
    matches: false,
    media: query,
    onchange: null,
    removeEventListener: vi.fn(),
    removeListener: vi.fn(),
  })),
});

Object.defineProperty(window, "ResizeObserver", {
  configurable: true,
  writable: true,
  value: ResizeObserverMock,
});

Object.defineProperty(globalThis, "ResizeObserver", {
  configurable: true,
  writable: true,
  value: ResizeObserverMock,
});

// jsdom has no layout engine, so window.scrollTo is unimplemented and every
// router scroll restoration emits a "Not implemented" jsdomError. Those bypass
// `silent: "passed-only"` (which only suppresses intercepted console output),
// so stub them to keep `make test` readable.
Object.defineProperty(window, "scrollTo", {
  configurable: true,
  writable: true,
  value: vi.fn(),
});

// Same story for canvas: jsdom ships no rasterizer, so getContext() is
// unimplemented and emits a jsdomError. @xterm/xterm calls it at module scope
// (common/Color.ts) just to parse CSS colours, so every test that imports the
// router — which statically pulls the terminal route — prints it. jsdom's stub
// returns null and callers already handle that, so return null silently. Not a
// vi.fn(): nothing asserts on it. Swap in a context object only once a test
// genuinely needs to draw.
Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  configurable: true,
  writable: true,
  value: () => null,
});

if (!globalThis.crypto?.randomUUID) {
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: {
      getRandomValues: (array: Uint8Array) => array.fill(1),
      randomUUID: () => "00000000-0000-4000-8000-000000000000",
    },
  });
}

afterEach(async () => {
  cleanup();
  window.localStorage.clear();
  window.sessionStorage.clear();
  vi.useRealTimers();
  vi.clearAllMocks();

  const { closeStreamMux } = await import("@/api/StreamMultiplexer");
  closeStreamMux();
});
