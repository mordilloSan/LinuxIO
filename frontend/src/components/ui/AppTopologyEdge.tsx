import type { CSSProperties } from "react";

import "./app-topology-edge.css";

// ponytail: repeating SVG tracks; individual particles only if flow simulation is needed.
const grainTracks = Array.from(
  { length: 12 },
  (_, index) =>
    ({
      transform: `translateY(${(index - 5.5) * 0.7}px)`,
      strokeWidth: 0.65 + (index % 3) * 0.15,
      strokeOpacity: 0.6 - Math.abs(index - 5.5) * 0.08,
      "--flow-offset": `${index * 17.3}px`,
      "--flow-duration": `${1.1 + (index % 5) * 0.11}s`,
    }) as CSSProperties,
);

interface Props {
  className: string;
  d: string;
  highlighted: boolean;
  paused: boolean;
  forward?: number;
  reverse?: number;
}

export default function AppTopologyEdge({
  className,
  d,
  highlighted,
  paused,
  forward = 0,
  reverse = 0,
}: Props) {
  return (
    <g
      className="app-topology-edge"
      data-highlighted={highlighted}
      data-paused={paused}
    >
      <path
        className={`app-topology-edge__line ${className}`}
        d={d}
        data-highlighted={highlighted}
        vectorEffect="non-scaling-stroke"
      />
      {(["forward", "reverse"] as const).map((direction) =>
        (direction === "forward" ? forward : reverse) > 0 ? (
          <g
            className="app-topology-edge__flow"
            data-direction={direction}
            key={direction}
          >
            <path
              className="app-topology-edge__ribbon"
              d={d}
              vectorEffect="non-scaling-stroke"
            />
            {grainTracks.map((style, index) => (
              <path
                className="app-topology-edge__grain"
                d={d}
                key={index}
                style={style}
                vectorEffect="non-scaling-stroke"
              />
            ))}
          </g>
        ) : null,
      )}
    </g>
  );
}
