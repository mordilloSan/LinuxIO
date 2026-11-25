import { createContext, type ReactNode, use, useState } from "react";

import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import { DOCK_INSET, DOCK_TABS } from "./dockLayout";

export const WIDTH_KEY = "linuxio.assistant.paneWidth";
export const DEFAULT_WIDTH = 340;
export const MIN_WIDTH = 240;
export const MAX_WIDTH = 720;

const isWidth = (value: unknown): value is number =>
  typeof value === "number" && value >= MIN_WIDTH && value <= MAX_WIDTH;

export const clamp = (value: number, min: number, max: number) =>
  Math.min(max, Math.max(min, value));

interface DockWidth {
  width: number;
  /** remember: the browser keeps the width, as when a drag ends. */
  setWidth: (width: number, remember: boolean) => void;
}

const DockWidthContext = createContext<DockWidth | null>(null);

/**
 * The one width every card of the dock has: the user widens or narrows them
 * all together, and the browser remembers it.
 */
export function DockWidthProvider({ children }: { children: ReactNode }) {
  const [width, setStored] = useState(
    () => readPersistedState(WIDTH_KEY, isWidth) ?? DEFAULT_WIDTH,
  );
  const setWidth = (next: number, remember: boolean) => {
    setStored(next);
    if (remember) writePersistedState(WIDTH_KEY, next);
  };
  return (
    <DockWidthContext value={{ width, setWidth }}>{children}</DockWidthContext>
  );
}

/**
 * The width a card is drawn at, and the widest the room beside the chat
 * lets it be (`room`: the width of the chat area; null until measured).
 */
export function useCardWidth(room: number | null) {
  const shared = use(DockWidthContext);
  if (!shared) throw new Error("a dock card needs a DockWidthProvider");
  const maxWidth =
    room === null
      ? MAX_WIDTH
      : clamp(room - DOCK_INSET - DOCK_TABS, MIN_WIDTH, MAX_WIDTH);
  return {
    width: Math.min(shared.width, maxWidth),
    maxWidth,
    setWidth: shared.setWidth,
  };
}
