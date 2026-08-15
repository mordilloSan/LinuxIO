import { useMotionValue } from "motion/react";
import { Children, type ReactNode } from "react";

import { useUpdateCanNavigate } from "@/hooks/useLinuxIOUpdater";

import DockActionSlot from "./DockActionSlot";
import DockItem from "./DockItem";
import { useSidebarItems } from "../sidebar/useSidebarItems";
import "./dock.css";

/* Per-route tile gradients (top → bottom), macOS-app-icon style. Routes
   without an entry fall back to cycling through this palette. */
const TILE_GRADIENTS: Record<string, readonly [string, string]> = {
  "/": ["#4fa8f8", "#1670e0"],
  "/accounts": ["#ff86a3", "#e83e64"],
  "/docker": ["#57a8ff", "#1d63ed"],
  "/filebrowser/$": ["#ffd968", "#f2a93b"],
  "/hardware": ["#6f8fb8", "#3c5a80"],
  "/logs": ["#aab4c2", "#6b7686"],
  "/network": ["#3fd4e0", "#0aa3b5"],
  "/services": ["#b57af2", "#7c3fd6"],
  "/shares": ["#7f96f7", "#4356d6"],
  "/storage": ["#ff9f5a", "#ee6c1d"],
  "/terminal": ["#43494f", "#17191c"],
  "/updates": ["#63de74", "#22a83a"],
};

const FALLBACK_GRADIENTS = Object.values(TILE_GRADIENTS);

const gradientFor = (to: string, index: number) =>
  TILE_GRADIENTS[to] ?? FALLBACK_GRADIENTS[index % FALLBACK_GRADIENTS.length];

/* Tile gradients for the relocated header actions, assigned by child order
   (notifications, theme toggle, settings, account). */
const ACTION_GRADIENTS: readonly (readonly [string, string])[] = [
  ["#ffb74a", "#f57c00"],
  ["#8e7cf0", "#5b45d6"],
  ["#aab4c2", "#6b7686"],
  ["#ff7b7b", "#e03131"],
];

export interface DockProps {
  /** Header action buttons shown after the nav tiles, behind a divider. */
  children?: ReactNode;
}

const Dock = ({ children }: DockProps) => {
  const items = useSidebarItems();
  const canNavigate = useUpdateCanNavigate();
  /* Cursor x position over the dock; Infinity = cursor away, all tiles at
     rest. Each item derives its own magnification from this single value. */
  const mouseX = useMotionValue(Infinity);

  return (
    <nav
      aria-label="Primary navigation"
      className="app-dock"
      onMouseLeave={() => mouseX.set(Infinity)}
      onMouseMove={(e) => mouseX.set(e.clientX)}
    >
      <ul className="app-dock__list">
        {items.map((page, index) => (
          <DockItem
            {...page}
            disabled={!canNavigate}
            gradient={gradientFor(page.to, index)}
            key={page.title}
            mouseX={mouseX}
          />
        ))}
      </ul>
      {children && (
        <div className="app-dock__actions">
          {Children.map(children, (child, index) => (
            <DockActionSlot
              gradient={ACTION_GRADIENTS[index % ACTION_GRADIENTS.length]}
              mouseX={mouseX}
            >
              {child}
            </DockActionSlot>
          ))}
        </div>
      )}
    </nav>
  );
};

export default Dock;
