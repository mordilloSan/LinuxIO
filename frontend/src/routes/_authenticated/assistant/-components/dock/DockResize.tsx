import type { KeyboardEvent, PointerEvent } from "react";
import { useRef, useState } from "react";

import { clamp, DEFAULT_WIDTH, MIN_WIDTH } from "./dockWidth";

import "../pane/pane.css";

const KEY_STEP = 16;

/**
 * The left edge of a card, which the user drags (or moves with the arrow
 * keys) to widen or narrow every card of the dock.
 */
export default function DockResize({
  title,
  width,
  maxWidth,
  setWidth,
}: {
  title: string;
  /** The width the card is drawn at. */
  width: number;
  maxWidth: number;
  setWidth: (width: number, remember: boolean) => void;
}) {
  const drag = useRef<{ x: number; width: number; latest: number } | null>(
    null,
  );
  const [dragging, setDragging] = useState(false);

  const resizeTo = (next: number, remember: boolean) => {
    const clamped = clamp(Math.round(next), MIN_WIDTH, maxWidth);
    setWidth(clamped, remember);
    return clamped;
  };
  const startDrag = (event: PointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    drag.current = { x: event.clientX, width, latest: width };
    setDragging(true);
  };
  const moveDrag = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current;
    if (!start) return;
    // The cards are anchored on the right, so dragging left widens them.
    start.latest = resizeTo(start.width + start.x - event.clientX, false);
  };
  const endDrag = () => {
    const start = drag.current;
    if (!start) return;
    drag.current = null;
    setDragging(false);
    setWidth(start.latest, true);
  };
  const resizeByKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const next =
      event.key === "ArrowLeft"
        ? width + KEY_STEP
        : event.key === "ArrowRight"
          ? width - KEY_STEP
          : event.key === "Home"
            ? MIN_WIDTH
            : event.key === "End"
              ? maxWidth
              : null;
    if (next === null) return;
    event.preventDefault();
    resizeTo(next, true);
  };

  return (
    <div
      aria-label={`Resize ${title}`}
      aria-orientation="vertical"
      aria-valuemax={maxWidth}
      aria-valuemin={MIN_WIDTH}
      aria-valuenow={width}
      className="assistant-pane-card__resize"
      data-dragging={dragging}
      onDoubleClick={() => resizeTo(DEFAULT_WIDTH, true)}
      onKeyDown={resizeByKey}
      onLostPointerCapture={endDrag}
      onPointerDown={startDrag}
      onPointerMove={moveDrag}
      onPointerUp={endDrag}
      role="separator"
      tabIndex={0}
    />
  );
}
