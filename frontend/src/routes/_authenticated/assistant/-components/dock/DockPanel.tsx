import {
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppTypography from "@/components/ui/AppTypography";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import { DOCK_INSET, DOCK_TABS } from "./dockLayout";
import DockTab from "./DockTab";

import "../pane/pane.css";

const WIDTH_KEY = "linuxio.assistant.paneWidth";
const DEFAULT_WIDTH = 340;
const MIN_WIDTH = 240;
const MAX_WIDTH = 720;
const KEY_STEP = 16;
// Mirrors the reading column (--assistant-column) in assistant.css.
const READING_COLUMN = 860;

const isWidth = (value: unknown): value is number =>
  typeof value === "number" && value >= MIN_WIDTH && value <= MAX_WIDTH;

const clamp = (value: number, min: number, max: number) =>
  Math.min(max, Math.max(min, value));

/**
 * A live view on a card that floats in the margin beside the chat, so the
 * conversation stays centred, with the tab on the chat's edge that opens and
 * closes it. The card is as wide as the user dragged it; where the margin
 * cannot hold it, it starts closed and opens over the chat on request.
 */
export default function DockPanel({
  title,
  drawing,
  room,
  open,
  onOpenChange,
  running,
  drawClosed = true,
  children,
}: {
  title: string;
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
  children: ReactNode;
}) {
  const tabRef = useRef<HTMLButtonElement>(null);
  const ringRef = useRef<HTMLSpanElement>(null);
  const glowRef = useRef<HTMLSpanElement>(null);
  const wasRunning = useRef(running);
  const drawn = useRef<unknown>(null);
  const drag = useRef<{ x: number; width: number; latest: number } | null>(
    null,
  );
  const [width, setWidth] = useState(
    () => readPersistedState(WIDTH_KEY, isWidth) ?? DEFAULT_WIDTH,
  );
  const [dragging, setDragging] = useState(false);

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

  const maxWidth =
    room === null
      ? MAX_WIDTH
      : clamp(room - DOCK_INSET - DOCK_TABS, MIN_WIDTH, MAX_WIDTH);
  const shownWidth = Math.min(width, maxWidth);
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

  const resizeTo = (next: number) => {
    const clamped = clamp(Math.round(next), MIN_WIDTH, maxWidth);
    setWidth(clamped);
    return clamped;
  };

  const startDrag = (event: PointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    drag.current = { x: event.clientX, width: shownWidth, latest: shownWidth };
    setDragging(true);
  };
  const moveDrag = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current;
    if (!start) return;
    // The card is anchored on the left, so dragging right widens it.
    start.latest = resizeTo(start.width + event.clientX - start.x);
  };
  const endDrag = () => {
    const start = drag.current;
    if (!start) return;
    drag.current = null;
    setDragging(false);
    writePersistedState(WIDTH_KEY, start.latest);
  };
  const resizeByKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const next =
      event.key === "ArrowRight"
        ? shownWidth + KEY_STEP
        : event.key === "ArrowLeft"
          ? shownWidth - KEY_STEP
          : event.key === "Home"
            ? MIN_WIDTH
            : event.key === "End"
              ? maxWidth
              : null;
    if (next === null) return;
    event.preventDefault();
    writePersistedState(WIDTH_KEY, resizeTo(next));
  };

  return (
    <>
      <DockTab
        glowRef={glowRef}
        label={title}
        onClick={() => onOpenChange(!shown)}
        open={shown}
        ref={tabRef}
        ringRef={ringRef}
        running={running}
      />
      <aside
        aria-hidden={!shown}
        aria-label={title}
        className={[
          "assistant-dock-card",
          "assistant-pane-card",
          !shown && "assistant-dock-card--closed",
          dragging && "assistant-pane-card--dragging",
        ]
          .filter(Boolean)
          .join(" ")}
        inert={!shown}
        style={{ width: shownWidth }}
      >
        <div
          aria-label={`Resize ${title}`}
          aria-orientation="vertical"
          aria-valuemax={maxWidth}
          aria-valuemin={MIN_WIDTH}
          aria-valuenow={shownWidth}
          className="assistant-pane-card__resize"
          onDoubleClick={() =>
            writePersistedState(WIDTH_KEY, resizeTo(DEFAULT_WIDTH))
          }
          onKeyDown={resizeByKey}
          onLostPointerCapture={endDrag}
          onPointerDown={startDrag}
          onPointerMove={moveDrag}
          onPointerUp={endDrag}
          role="separator"
          tabIndex={0}
        />
        <header className="assistant-dock-card__header">
          <span aria-hidden className="assistant-pane-card__live" />
          <AppTypography
            className="assistant-dock-card__title"
            fontWeight={600}
            noWrap
          >
            {title}
          </AppTypography>
          <AppActionIconButton
            ariaLabel={`Close ${title}`}
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
    </>
  );
}
