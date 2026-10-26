import "./app-topology-edge.css";

const MB = 1024 ** 2;

/** Speed and brightness class for a rate in bytes per second. */
export const flowTier = (bytesPerSec: number) =>
  bytesPerSec <= 0
    ? "idle"
    : bytesPerSec < 10 * MB
      ? "light"
      : bytesPerSec < 100 * MB
        ? "medium"
        : "busy";

interface Props {
  className: string;
  d: string;
  highlighted: boolean;
  paused: boolean;
  /** Draw the streams even at 0 B/s: the link is mounted or running. */
  live?: boolean;
  /** Bytes per second along the path, drawn as the forward stream. */
  forward?: number;
  /** Bytes per second against the path, drawn as the stream back. */
  reverse?: number;
}

/**
 * One topology connection: a base line and, when live, a dashed read stream
 * towards the target and a dashed write stream back, moving at a pace set
 * by the larger rate. Strokes are in screen pixels whatever the viewBox.
 */
export default function AppTopologyEdge({
  className,
  d,
  highlighted,
  paused,
  live = false,
  forward = 0,
  reverse = 0,
}: Props) {
  return (
    <g
      className="app-topology-edge"
      data-highlighted={highlighted}
      data-paused={paused}
      data-tier={live ? flowTier(Math.max(forward, reverse)) : undefined}
    >
      <path
        className={`app-topology-edge__line ${className}`}
        d={d}
        data-highlighted={highlighted}
        vectorEffect="non-scaling-stroke"
      />
      {live && (
        <path
          className="app-topology-edge__flow"
          d={d}
          data-direction="forward"
          vectorEffect="non-scaling-stroke"
        />
      )}
      {live && reverse > 0 && (
        <path
          className="app-topology-edge__flow"
          d={d}
          data-direction="reverse"
          vectorEffect="non-scaling-stroke"
        />
      )}
    </g>
  );
}
