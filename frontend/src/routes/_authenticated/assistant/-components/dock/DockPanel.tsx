import { type ReactNode, useEffect, useRef } from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppTypography from "@/components/ui/AppTypography";

import DockCard from "./DockCard";
import { DOCK_INSET, DOCK_TABS } from "./dockLayout";
import DockResize from "./DockResize";
import DockTab from "./DockTab";
import { useCardWidth } from "./dockWidth";

import "../pane/pane.css";

// Mirrors the reading column (--assistant-column) in assistant.css.
const READING_COLUMN = 860;

/**
 * A live view on a card that floats in the margin beside the chat, so the
 * conversation stays centred, with the tab on the chat's edge that opens and
 * closes it. The card is as wide as the user dragged it; where the margin
 * cannot hold it, it starts closed and opens over the chat on request.
 */
export default function DockPanel({
  title,
  heading = title,
  drawing,
  room,
  open,
  onOpenChange,
  running,
  drawClosed = true,
  order,
  quiet = false,
  children,
}: {
  title: string;
  /** The card's name while it is open, when it is not the tab's. */
  heading?: string;
  /** What is drawn: the tab's dot ripples each time this changes. */
  drawing: unknown;
  /** The width of the chat area the card floats in; null until measured. */
  room: number | null;
  /** null follows the room: open where the card fits beside the chat. */
  open: boolean | null;
  onOpenChange: (open: boolean) => void;
  /** A turn is running: the tab's dot pulses, and glows when it ends. */
  running: boolean;
  /** false leaves the content out of the page while the card is closed. */
  drawClosed?: boolean;
  /** Where the card stands in the stack of open cards: the lower, the higher. */
  order?: number;
  /** The tab's dot is grey: nothing it stands for is at work. */
  quiet?: boolean;
  children: ReactNode;
}) {
  const tabRef = useRef<HTMLButtonElement>(null);
  const ringRef = useRef<HTMLSpanElement>(null);
  const glowRef = useRef<HTMLSpanElement>(null);
  const wasRunning = useRef(running);
  const drawn = useRef<unknown>(null);
  const { width: shownWidth, maxWidth, setWidth } = useCardWidth(room);

  // One ripple from the tab's dot each time the view is redrawn.
  useEffect(() => {
    if (drawn.current === drawing) return;
    drawn.current = drawing;
    const ring = ringRef.current;
    if (!ring || typeof ring.animate !== "function") return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    const ripple = ring.animate(
      [
        { opacity: 0.9, transform: "scale(1)" },
        { opacity: 0, transform: "scale(3)" },
      ],
      { duration: 700, easing: "ease-out" },
    );
    return () => ripple.cancel();
  }, [drawing]);

  const fits =
    room === null ||
    room >= READING_COLUMN + 2 * (shownWidth + DOCK_INSET + DOCK_TABS);
  const shown = open ?? fits;

  // The tab glows twice when a turn ends while the card is closed.
  useEffect(() => {
    const finished = wasRunning.current && !running;
    wasRunning.current = running;
    const glow = glowRef.current;
    if (!finished || shown || !glow || typeof glow.animate !== "function") {
      return;
    }
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    const lit = glow.animate(
      [
        { opacity: 0.45, transform: "scale(1)" },
        { opacity: 0, transform: "scale(1.5, 1.2)" },
      ],
      { duration: 900, easing: "ease-out", iterations: 2 },
    );
    return () => lit.cancel();
  }, [running, shown]);

  return (
    <>
      <DockTab
        glowRef={glowRef}
        label={title}
        onClick={() => onOpenChange(!shown)}
        open={shown}
        quiet={quiet}
        ref={tabRef}
        ringRef={ringRef}
        running={running}
      />
      <DockCard>
        <aside
          aria-hidden={!shown}
          aria-label={heading}
          className={[
            "assistant-dock-card",
            "assistant-pane-card",
            !shown && "assistant-dock-card--closed",
          ]
            .filter(Boolean)
            .join(" ")}
          inert={!shown}
          style={{ width: shownWidth, order }}
        >
          <DockResize
            maxWidth={maxWidth}
            setWidth={setWidth}
            title={title}
            width={shownWidth}
          />
          <header className="assistant-dock-card__header">
            <span aria-hidden className="assistant-pane-card__live" />
            <AppTypography
              className="assistant-dock-card__title"
              fontWeight={600}
              noWrap
            >
              {heading}
            </AppTypography>
            <AppActionIconButton
              ariaLabel={`Close ${heading}`}
              icon="mdi:close"
              onClick={() => {
                onOpenChange(false);
                // The card goes inert; its tab is where it went.
                tabRef.current?.focus();
              }}
              size="small"
              tooltip={false}
            />
          </header>
          <div className="assistant-pane-card__body">
            {shown || drawClosed ? children : null}
          </div>
        </aside>
      </DockCard>
    </>
  );
}
