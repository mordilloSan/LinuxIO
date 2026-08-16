import { useEffect } from "react";

import { useUpdateCanNavigate } from "@/hooks/useLinuxIOUpdater";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import DockItem from "./DockItem";
import {
  DockMagnificationProvider,
  useDockPointer,
} from "./useDockMagnification";
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
  "/settings": ["#c3ccd8", "#5f6a78"],
  "/shares": ["#7f96f7", "#4356d6"],
  "/storage": ["#ff9f5a", "#ee6c1d"],
  "/terminal": ["#43494f", "#17191c"],
  "/updates": ["#63de74", "#22a83a"],
};

const FALLBACK_GRADIENTS = Object.values(TILE_GRADIENTS);

const gradientFor = (to: string, index: number) =>
  TILE_GRADIENTS[to] ?? FALLBACK_GRADIENTS[index % FALLBACK_GRADIENTS.length];

const Dock = () => {
  const items = useSidebarItems();
  const canNavigate = useUpdateCanNavigate();
  const setPointer = useDockPointer();
  const theme = useAppTheme();
  const magnificationEnabled = useAppMediaQuery(theme.breakpoints.up("sm"));

  useEffect(() => {
    if (!magnificationEnabled) setPointer(Infinity);
  }, [magnificationEnabled, setPointer]);

  return (
    <nav
      aria-label="Primary navigation"
      className="app-dock"
      onMouseLeave={() => setPointer(Infinity)}
      onMouseMove={
        magnificationEnabled ? (event) => setPointer(event.clientX) : undefined
      }
    >
      <ul className="app-dock__list">
        {items.map((page, index) => (
          <DockItem
            {...page}
            disabled={!canNavigate}
            gradient={gradientFor(page.to, index)}
            key={page.title}
          />
        ))}
      </ul>
    </nav>
  );
};

const DockWithMagnification = () => (
  <DockMagnificationProvider>
    <Dock />
  </DockMagnificationProvider>
);

export default DockWithMagnification;
