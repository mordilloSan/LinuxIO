import { useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import DockTile from "@/routes/_authenticated/-components/dock/DockTile";
import {
  DockMagnificationProvider,
  useDockPointerLiveness,
} from "@/routes/_authenticated/-components/dock/useDockMagnification";

import "@/routes/_authenticated/-components/dock/dock.css";

/* The production dock structure with the production pointer-liveness wiring,
   so the browser tests exercise the same hover-label gate Dock.tsx uses. */
function DockFixture() {
  const { navRef, onPointerDown, onPointerLeave, onPointerMove } =
    useDockPointerLiveness();

  return (
    <nav
      aria-label="Dock fixture"
      className="app-dock"
      onPointerLeave={onPointerLeave}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      ref={navRef}
    >
      <ul className="app-dock__list">
        <li className="app-dock__item">
          <a
            aria-label="Dashboard"
            className="app-dock-link"
            data-testid="dock-dashboard"
            href="#dashboard"
          >
            <DockTile gradient={["#4fa8f8", "#1670e0"]} label="Dashboard">
              <span aria-hidden="true">⌂</span>
            </DockTile>
          </a>
        </li>
      </ul>
    </nav>
  );
}

export default function AccessibilityPage() {
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [popoverPosition, setPopoverPosition] = useState({
    top: 100,
    left: 100,
  });
  const [largePopover, setLargePopover] = useState(false);
  const [activations, setActivations] = useState({
    button: 0,
    chip: 0,
    icon: 0,
  });
  const activate = (control: keyof typeof activations) =>
    setActivations((current) => ({
      ...current,
      [control]: current[control] + 1,
    }));

  return (
    <main style={{ minHeight: "200vh", padding: "32px" }}>
      <h1>Accessibility fixture</h1>
      <p>Use Tab to move through each interactive control.</p>
      <div style={{ display: "flex", gap: "16px", alignItems: "center" }}>
        <AppButton
          aria-label="Activate button"
          onClick={() => activate("button")}
        >
          Button
        </AppButton>
        <AppIconButton
          aria-label="Activate icon button"
          onClick={() => activate("icon")}
        >
          <span aria-hidden="true">✦</span>
        </AppIconButton>
        <AppChip
          color="primary"
          label="Activate chip"
          onClick={() => activate("chip")}
        />
      </div>
      <output aria-live="polite" data-testid="activation-counts">
        Button: {activations.button}; Icon: {activations.icon}; Chip:{" "}
        {activations.chip}
      </output>
      <DockMagnificationProvider>
        <DockFixture />
      </DockMagnificationProvider>
      {/* Tooltip triggers stay below the dock so the Tab counts above hold. */}
      <div style={{ display: "flex", gap: "16px", marginTop: "24px" }}>
        <AppTooltip title="Collapse row">
          <AppButton>Tooltip button</AppButton>
        </AppTooltip>
        <AppTooltip title="Search syntax">
          <AppTextField aria-label="Tooltip query" />
        </AppTooltip>
      </div>
      <div style={{ position: "fixed", top: "8.25px", right: "8.25px" }}>
        <AppTooltip arrow placement="top" title="Notifications">
          <AppIconButton aria-label="Notification tooltip">
            <span aria-hidden="true">♧</span>
          </AppIconButton>
        </AppTooltip>
      </div>
      <div style={{ position: "fixed", top: "96px", right: "8px" }}>
        <AppTooltip
          contentWidth
          placement="top"
          title={`Monitoring details\n${"Device status unavailable\n".repeat(80)}`}
        >
          <AppButton>Long monitoring details</AppButton>
        </AppTooltip>
      </div>
      <AppButton onClick={() => setPopoverOpen(true)}>
        Open positioning fixture
      </AppButton>
      <AppPopover anchorPosition={popoverPosition} open={popoverOpen}>
        <div
          style={{
            width: largePopover ? 400 : 180,
            height: largePopover ? 500 : 120,
          }}
        >
          <AppButton
            onClick={() => setPopoverPosition({ top: 200, left: 200 })}
          >
            Move popover
          </AppButton>
          <AppButton onClick={() => setLargePopover(true)}>
            Grow content
          </AppButton>
        </div>
      </AppPopover>
      <div style={{ height: "1600px" }} />
    </main>
  );
}
