import { DndContext } from "@dnd-kit/core";
import {
  horizontalListSortingStrategy,
  SortableContext,
} from "@dnd-kit/sortable";
import type { CSSProperties } from "react";

import type { ConfigDockAccentGradient, DockTileColors } from "@/api";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import { useAppUpdateInProgress } from "@/hooks/useAppUpdater";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";
import { fromHsl, lighten, toHsl } from "@/theme/colorUtils";

import DockItem from "./DockItem";
import { sampleDockAccentColor, type TileGradient } from "./dockPalette";
import {
  DockMagnificationProvider,
  useDockPointerLiveness,
} from "./useDockMagnification";
import type { SidebarItem } from "../sidebar/types";
import { useSidebarItems } from "../sidebar/useSidebarItems";

import "@/components/reorder/reorder.css";
import "./dock.css";

/* Fixed per-route gradients (top → bottom), macOS-app-icon style. Routes
   without an entry fall back to cycling through this palette. Owes nothing to
   the theme, which is why it is no longer the default — see the accent
   palettes below. */
const VIBRANT_GRADIENTS: Record<string, TileGradient> = {
  "/": ["#4fa8f8", "#1670e0"],
  "/accounts": ["#ff86a3", "#e83e64"],
  "/docker": ["#57a8ff", "#1d63ed"],
  "/filebrowser/$": ["#ffd968", "#f2a93b"],
  "/hardware": ["#6f8fb8", "#3c5a80"],
  "/logs": ["#aab4c2", "#6b7686"],
  "/network": ["#3fd4e0", "#0aa3b5"],
  "/services": ["#b57af2", "#7c3fd6"],
  "/settings": ["#c3ccd8", "#5f6a78"],
  "/storage": ["#ff9f5a", "#ee6c1d"],
  "/terminal": ["#43494f", "#17191c"],
  "/updates": ["#63de74", "#22a83a"],
};

const VIBRANT_FALLBACK = Object.values(VIBRANT_GRADIENTS);

/* Neutral tiles keep a trace of the accent hue rather than being flat gray, and
   are clamped to a lightness band that keeps a white icon legible whatever
   accent the user picked. */
const NEUTRAL_SATURATION = 0.08;
const NEUTRAL_MIN_LIGHTNESS = 0.38;
const NEUTRAL_MAX_LIGHTNESS = 0.55;

/* A lit face over the base color: the same 26% white top that the notification
   icons use (.app-navbar-notifications__icon), so a status icon and a dock tile
   read as one object at two sizes. */
const tileGradient = (base: string): TileGradient => [
  lighten(base, 0.26),
  base,
];

const neutralBase = (accent: string) => {
  const hsl = toHsl(accent);
  if (!hsl) return "#6b7686";
  return fromHsl(
    hsl.h,
    NEUTRAL_SATURATION,
    Math.min(NEUTRAL_MAX_LIGHTNESS, Math.max(NEUTRAL_MIN_LIGHTNESS, hsl.l)),
  );
};

const accentFamilyGradient = (
  accent: string,
  gradient: ConfigDockAccentGradient | undefined,
  index: number,
  count: number,
): TileGradient => {
  const position = count < 2 ? 0.5 : index / (count - 1);
  return tileGradient(sampleDockAccentColor(accent, gradient, position));
};

const gradientFor = (
  palette: DockTileColors,
  accent: string,
  accentGradient: ConfigDockAccentGradient | undefined,
  to: string,
  index: number,
  count: number,
): TileGradient => {
  switch (palette) {
    case "vibrant":
      return (
        VIBRANT_GRADIENTS[to] ??
        VIBRANT_FALLBACK[index % VIBRANT_FALLBACK.length]
      );
    case "mono":
      return tileGradient(accent);
    case "neutral":
      return tileGradient(neutralBase(accent));
    default:
      return accentFamilyGradient(accent, accentGradient, index, count);
  }
};

const getNavItemId = (item: SidebarItem) => item.to;

const Dock = () => {
  const canNavigate = !useAppUpdateInProgress();
  const surface = useReorderableSurface({
    disabled: !canNavigate,
    getId: getNavItemId,
    items: useSidebarItems(),
    surface: "navigation.dock",
  });
  const { items } = surface;
  const theme = useAppTheme();
  const [dockTileColors] = useConfigValue("dockTileColors");
  const [dockAccentGradient] = useConfigValue("dockAccentGradient");
  const { navRef, onPointerDown, onPointerLeave, onPointerMove } =
    useDockPointerLiveness();

  const palette = dockTileColors;
  const accent = theme.palette.primary.main;

  /* Only the neutral palette lights the current route differently, and which
     tile that is already lives in CSS as .app-dock-link--active. Publishing the
     accent pair here lets that rule do the swap without the dock having to
     track the active route itself. */
  const activeGradient = palette === "neutral" ? tileGradient(accent) : null;

  return (
    <nav
      aria-label="Primary navigation"
      className="app-dock"
      /* Magnification measures tile centres lazily and would keep stale ones
         across a rearrangement, so layout mode parks it at rest; the reset
         also marks the layout for a fresh measure once the mode closes. */
      onPointerDown={surface.editMode ? onPointerLeave : onPointerDown}
      onPointerLeave={onPointerLeave}
      onPointerMove={surface.editMode ? onPointerLeave : onPointerMove}
      ref={navRef}
      style={
        activeGradient
          ? ({
              "--dock-tile-active-top": activeGradient[0],
              "--dock-tile-active-bottom": activeGradient[1],
            } as CSSProperties)
          : undefined
      }
    >
      <DndContext {...surface.dndContextProps}>
        <SortableContext
          items={surface.ids}
          strategy={horizontalListSortingStrategy}
        >
          <ul
            className={
              surface.editMode
                ? "app-dock__list app-dock__list--reordering"
                : "app-dock__list"
            }
          >
            {items.map((page, index) => (
              <DockItem
                {...page}
                disabled={!canNavigate}
                editMode={surface.editMode}
                gradient={gradientFor(
                  palette,
                  accent,
                  dockAccentGradient,
                  page.to,
                  index,
                  items.length,
                )}
                key={page.title}
                pending={surface.pendingId === page.to}
              />
            ))}
          </ul>
        </SortableContext>
      </DndContext>
    </nav>
  );
};

const DockWithMagnification = () => (
  <DockMagnificationProvider>
    <Dock />
  </DockMagnificationProvider>
);

export default DockWithMagnification;
