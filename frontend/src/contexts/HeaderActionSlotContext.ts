import { createContext } from "react";

interface HeaderActionSlotValue {
  /**
   * Portal target sitting in the app header corner, immediately left of the
   * power menu. Null until the header has mounted it, so a consumer can fall
   * back to rendering in place (tests, and any shell without a navbar).
   */
  host: HTMLElement | null;
  /** Callback ref for the header element that owns the corner. */
  mount: (element: HTMLElement | null) => void;
}

export const HeaderActionSlotContext =
  createContext<HeaderActionSlotValue | null>(null);
