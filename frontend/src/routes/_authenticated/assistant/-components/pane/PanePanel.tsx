import {
  type KeyboardEvent,
  type PointerEvent,
  useEffect,
  useRef,
  useState,
} from "react";

import type { PaneView } from "@/api/acp";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppTypography from "@/components/ui/AppTypography";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import PaneTree from "./PaneTree";
import { paneTitle } from "./readPaneTree";

import "./pane.css";

const WIDTH_KEY = "linuxio.assistant.paneWidth";
const DEFAULT_WIDTH = 340;
const MIN_WIDTH = 240;
const MAX_WIDTH = 720;
const KEY_STEP = 16;
// Both mirror the stylesheets: the card's inset in pane.css and the reading
// column (--assistant-column) in assistant.css.
const INSET = 16;
const READING_COLUMN = 860;

const isWidth = (value: unknown): value is number =>
  typeof value === "number" && value >= MIN_WIDTH && value <= MAX_WIDTH;

const clamp = (value: number, min: number, max: number) =>
  Math.min(max, Math.max(min, value));

/**
 * A Claude Code plugin's pane, drawn read-only on a card that floats in the
 * margin beside the chat, so the conversation stays centred. The card is as
 * wide as the user dragged it; where the margin cannot hold it, it starts as
 * a pill and opens over the chat on request.
 */
export default function PanePanel({
  pane,
  onHide,
}: {
  pane: PaneView;
  onHide: () => void;
}) {
  const title = paneTitle(pane);
  const cardRef = useRef<HTMLElement>(null);
  const ringRef = useRef<HTMLSpanElement>(null);
  const drawn = useRef<PaneView["tree"] | null>(null);
  const drag = useRef<{ x: number; width: number; latest: number } | null>(
    null,
  );
  const [width, setWidth] = useState(
    () => readPersistedState(WIDTH_KEY, isWidth) ?? DEFAULT_WIDTH,
  );
  const [dragging, setDragging] = useState(false);
  // The width of the chat area the card floats in; unknown until measured.
  const [room, setRoom] = useState<number | null>(null);
  // null follows the room: open where the card fits beside the chat.
  const [minimizedByUser, setMinimizedByUser] = useState<boolean | null>(null);

  useEffect(() => {
    const area = cardRef.current?.parentElement;
    if (!area || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) =>
      setRoom(entry.contentRect.width),
    );
    observer.observe(area);
    return () => observer.disconnect();
  }, []);

  // One ripple from the header dot each time the plugin redraws.
  useEffect(() => {
    if (drawn.current === pane.tree) return;
    drawn.current = pane.tree;
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
  }, [pane.tree]);

  const maxWidth =
    room === null ? MAX_WIDTH : clamp(room - 2 * INSET, MIN_WIDTH, MAX_WIDTH);
  const shownWidth = Math.min(width, maxWidth);
  const fits =
    room === null || room >= READING_COLUMN + 2 * (shownWidth + 2 * INSET);
  const minimized = minimizedByUser ?? !fits;

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
    // The card is anchored on the right, so dragging left widens it.
    start.latest = resizeTo(start.width + start.x - event.clientX);
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
      event.key === "ArrowLeft"
        ? shownWidth + KEY_STEP
        : event.key === "ArrowRight"
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
    <aside
      aria-label={title}
      className={[
        "assistant-pane-card",
        minimized && "assistant-pane-card--minimized",
        dragging && "assistant-pane-card--dragging",
      ]
        .filter(Boolean)
        .join(" ")}
      ref={cardRef}
      style={minimized ? undefined : { width: shownWidth }}
    >
      {minimized ? null : (
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
      )}
      <header className="assistant-pane-card__header">
        <span aria-hidden className="assistant-pane-card__live">
          <span className="assistant-pane-card__ring" ref={ringRef} />
        </span>
        <AppTypography
          className="assistant-pane-card__title"
          fontWeight={600}
          noWrap
        >
          {title}
        </AppTypography>
        <AppActionIconButton
          ariaLabel={minimized ? `Open ${title}` : `Minimize ${title}`}
          icon={minimized ? "mdi:chevron-down" : "mdi:chevron-up"}
          onClick={() => setMinimizedByUser(!minimized)}
          size="small"
          tooltip={false}
        />
        <AppActionIconButton
          ariaLabel={`Hide ${title}`}
          icon="mdi:close"
          onClick={onHide}
          size="small"
          tooltip={false}
        />
      </header>
      {minimized ? null : (
        <div className="assistant-pane-card__body">
          <PaneTree tree={pane.tree} />
        </div>
      )}
    </aside>
  );
}
