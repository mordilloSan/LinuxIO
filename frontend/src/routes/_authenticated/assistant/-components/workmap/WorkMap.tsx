import {
  type CSSProperties,
  type RefObject,
  useEffect,
  useRef,
  useState,
} from "react";

import AppButton from "@/components/ui/AppButton";

import {
  LONG_LABEL,
  ROW_TEXT,
  ROW_TONE,
  rowLine,
  rowTime,
  STATIONS,
  type StationId,
  type StationTally,
  type WorkSummary,
} from "./workMap";

import "../pane/pane.css";
import "./work-map.css";

// The picture's own units; it scales with the card. The layout, the timings
// and the motion are lot-map's (see workMap.ts).
const W = 460;
const H = 430;
const CX = 230;
const CY = 212;
const RX = 176;
const RY = 164;
const NODE = 22;
const CORE = 40;
// Where spokes and pulses meet the core: outside the context ring.
const HUB = CORE + 8;

const PULSE_MS = 800;
const RIPPLE_MS = 450;
const GLOW_MS = 3200;
const ALARM_MS = 6000;
const DONE_MS = 1100;
// Calls that start in one go fly side by side, this many at most.
const LANES = 6;

type Point = { x: number; y: number };

const CENTRE: Point = { x: CX, y: CY };

const turn = (index: number) => (index * 2 * Math.PI) / STATIONS.length;
const at = (angle: number, factor = 1): Point => ({
  x: CX + RX * factor * Math.sin(angle),
  y: CY - RY * factor * Math.cos(angle),
});

/** Where a straight line from `from` to `to` meets the circle of radius `r` around `from`. */
const edge = (from: Point, to: Point, r: number): Point => {
  const length = Math.hypot(to.x - from.x, to.y - from.y) || 1;
  return {
    x: from.x + ((to.x - from.x) * r) / length,
    y: from.y + ((to.y - from.y) * r) / length,
  };
};

/** The same line moved sideways, so that several pulses on one spoke fly side by side. */
const aside = (a: Point, b: Point, off: number): [Point, Point] => {
  const length = Math.hypot(b.x - a.x, b.y - a.y) || 1;
  const dx = (-(b.y - a.y) * off) / length;
  const dy = ((b.x - a.x) * off) / length;
  return [
    { x: a.x + dx, y: a.y + dy },
    { x: b.x + dx, y: b.y + dy },
  ];
};

const n1 = (value: number) => (Math.round(value * 10) / 10).toString();

// Where each station sits and the two ends of its spoke.
const LAYOUT = STATIONS.map((_, index) => {
  const p = at(turn(index));
  return { p, hub: edge(CENTRE, p, HUB), rim: edge(p, CENTRE, NODE) };
});

const INDEX = Object.fromEntries(
  STATIONS.map((station, index) => [station.id, index]),
) as Record<StationId, number>;

// How far from the core a leg of the route bends, by how many stations apart
// its ends are: in enough to clear the labels, out enough to clear the core.
const BEND = [0.5, 0.5, 0.4, 0.45, 0.55, 0.75];

/** The way from one station to another: a curve through the room between the core and the ring. */
function leg(from: number, to: number): { path: string; end: Point } {
  const n = STATIONS.length;
  let steps = (((to - from) % n) + n) % n;
  if (steps > n / 2) steps -= n;
  const bend = at(turn(from + steps / 2), BEND[Math.abs(steps)] ?? 0.5);
  const a = edge(LAYOUT[from].p, bend, NODE + 4);
  const b = edge(LAYOUT[to].p, bend, NODE + 4);
  return {
    path: `M${n1(a.x)} ${n1(a.y)}Q${n1(bend.x)} ${n1(bend.y)} ${n1(b.x)} ${n1(b.y)}`,
    end: b,
  };
}

const canMove = (element: Element) =>
  typeof element.animate === "function" &&
  !window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

// ---- what flies: drawn for a moment, then taken out again -------------------

const SVG_NS = "http://www.w3.org/2000/svg";

function play(
  layer: SVGGElement,
  tag: "circle" | "line",
  className: string,
  attributes: Record<string, string | number>,
  frames: Keyframe[],
  timing: KeyframeAnimationOptions,
) {
  const element = document.createElementNS(SVG_NS, tag);
  element.setAttribute("class", className);
  for (const [name, value] of Object.entries(attributes)) {
    element.setAttribute(name, String(value));
  }
  layer.append(element);
  const remove = () => element.remove();
  element.animate(frames, timing).finished.then(remove, remove);
}

const move = ({ x, y }: Point) => `translate(${n1(x)}px, ${n1(y)}px)`;

/** A pulse with a short tail behind it. A hollow one is a signal nobody asked for. */
function pulse(
  layer: SVGGElement,
  from: Point,
  to: Point,
  tone: string,
  radius: number,
  hollow = false,
) {
  const near = {
    x: from.x + (to.x - from.x) * 0.82,
    y: from.y + (to.y - from.y) * 0.82,
  };
  for (let k = hollow ? 0 : 2; k >= 0; k--) {
    const peak = 1 - 0.36 * k;
    play(
      layer,
      "circle",
      `work-map__pulse${hollow ? " work-map__pulse--hollow" : ""} ${tone}`,
      { r: n1(radius * (1 - 0.27 * k)) },
      [
        { opacity: peak, transform: move(from) },
        { opacity: peak, offset: 0.82, transform: move(near) },
        { opacity: 0, transform: move(to) },
      ],
      { delay: k * 45, duration: PULSE_MS },
    );
  }
}

/** A ring that spreads from where a pulse lands, at the moment it lands. */
function ripple(layer: SVGGElement, p: Point, r: number, tone: string) {
  play(
    layer,
    "circle",
    `work-map__ripple ${tone}`,
    { cx: n1(p.x), cy: n1(p.y), r },
    [
      { opacity: 0.7, transform: "scale(1)" },
      { opacity: 0, transform: `scale(${(r + 11) / r})` },
    ],
    { delay: PULSE_MS, duration: RIPPLE_MS },
  );
}

/** The spoke lights up for a moment when something starts along it. */
function beam(
  layer: SVGGElement,
  a: Point,
  b: Point,
  tone: string,
  dashed = false,
) {
  play(
    layer,
    "line",
    `work-map__beam${dashed ? " work-map__beam--dashed" : ""} ${tone}`,
    { x1: n1(a.x), y1: n1(a.y), x2: n1(b.x), y2: n1(b.y) },
    [{ opacity: 0.75 }, { opacity: 0 }],
    { duration: GLOW_MS / 2 },
  );
}

const lane = (index: number, of: number) => (index - (of - 1) / 2) * 5;

/**
 * Plays what happened since the last drawing: a pulse out for each call that
 * started, one back for each that ended, a hollow one in for a prompt, and a
 * wide ring from the core for an answer. Only what was seen happening counts,
 * so a chat that is loaded plays nothing.
 */
function useFlights(
  under: RefObject<SVGGElement | null>,
  over: RefObject<SVGGElement | null>,
  tally: WorkSummary["tally"],
) {
  const drawn = useRef(tally);
  useEffect(() => {
    const was = drawn.current;
    drawn.current = tally;
    const layer = under.current;
    const top = over.current;
    if (!layer || !top || was === tally || !canMove(layer)) return;
    STATIONS.forEach((station, index) => {
      const { p, hub, rim } = LAYOUT[index];
      const before = was[station.id].seen;
      const now = tally[station.id].seen;
      const out = Math.min(LANES, now.out - before.out);
      const back = Math.min(LANES, now.back - before.back);
      const failed = now.failed - before.failed;
      const own = station.id === "memory" ? " work-map__tone--memory" : "";
      const main = `work-map__tone--main${own}`;
      const hot = `work-map__tone--hot${own}`;

      if (station.id === "you") {
        // A prompt comes in from the person; an answer goes out to them.
        for (let j = 0; j < back; j++) {
          const [a, b] = aside(hub, rim, lane(j, back));
          beam(layer, hub, rim, main, true);
          pulse(layer, b, a, hot, 3.8, true);
          ripple(layer, CENTRE, HUB - 2, hot);
        }
        for (let j = 0; j < out; j++) {
          beam(layer, hub, rim, main);
          pulse(layer, hub, rim, hot, 3.8);
          ripple(layer, p, NODE, hot);
          play(
            top,
            "circle",
            "work-map__ripple work-map__ripple--done work-map__tone--bone",
            { cx: CX, cy: CY, r: CORE },
            [
              { opacity: 0.6, transform: "scale(1)" },
              { opacity: 0, transform: `scale(${(CORE + 38) / CORE})` },
            ],
            { duration: DONE_MS },
          );
        }
        return;
      }
      for (let j = 0; j < out; j++) {
        const [a, b] = aside(hub, rim, lane(j, out));
        beam(layer, hub, rim, main);
        pulse(layer, a, b, hot, 3.8);
        ripple(layer, p, NODE, hot);
      }
      for (let j = 0; j < back; j++) {
        const [a, b] = aside(hub, rim, lane(j, back));
        pulse(
          layer,
          b,
          a,
          j < failed ? "work-map__tone--red" : "work-map__tone--ash",
          2.6,
        );
      }
    });
  }, [under, over, tally]);
}

/**
 * A station stays lit for a while after something happened at it, then cools
 * back to the resting line; after a failure it stays red for longer.
 */
function useAfterglow(
  parts: {
    glow: RefObject<SVGCircleElement | null>;
    lit: RefObject<SVGCircleElement | null>;
    glyph: RefObject<SVGPathElement | null>;
    label: RefObject<SVGTextElement | null>;
  },
  activity: number,
  failed: number,
) {
  const { glow, lit, glyph, label } = parts;
  const seen = useRef({ activity, failed });
  useEffect(() => {
    const was = seen.current;
    seen.current = { activity, failed };
    const alarm = failed > was.failed;
    if (!alarm && activity <= was.activity) return;
    if (!glow.current || !canMove(glow.current)) return;
    const colour = alarm ? "var(--wm-red)" : "var(--wm-main)";
    const hot = alarm ? "var(--wm-red)" : "var(--wm-hot)";
    const duration = alarm ? ALARM_MS : GLOW_MS;
    const played = [
      glow.current.animate(
        [
          { fill: colour, opacity: 0.3 },
          { fill: colour, opacity: 0 },
        ],
        duration,
      ),
      lit.current?.animate(
        [
          { stroke: colour, opacity: 1 },
          { stroke: colour, opacity: 0 },
        ],
        duration,
      ),
      glyph.current?.animate([{ stroke: hot }, { stroke: hot }], duration),
      label.current?.animate(
        [{ fill: "var(--wm-bone)" }, { fill: "var(--wm-bone)" }],
        duration,
      ),
    ];
    return () => played.forEach((animation) => animation?.cancel());
  }, [glow, lit, glyph, label, activity, failed]);
}

function Station({
  station,
  index,
  tally,
}: {
  station: (typeof STATIONS)[number];
  index: number;
  tally: StationTally;
}) {
  const { p } = LAYOUT[index];
  const glow = useRef<SVGCircleElement>(null);
  const lit = useRef<SVGCircleElement>(null);
  const glyph = useRef<SVGPathElement>(null);
  const label = useRef<SVGTextElement>(null);
  useAfterglow(
    { glow, lit, glyph, label },
    tally.seen.out + tally.seen.back,
    tally.seen.failed,
  );

  const running = tally.open > 0;
  const corner = NODE * 0.74;
  const upright = `rotate(-90 ${n1(p.x)} ${n1(p.y)})`;

  return (
    <g
      className={[
        "work-map__station",
        station.id === "memory" && "work-map__station--memory",
        station.id === "agents" && "work-map__station--agents",
        tally.count > 0 && "work-map__station--counted",
        running && "work-map__station--running",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <title>
        {`${station.label}: ${tally.count}${tally.faults > 0 ? `, ${tally.faults} failed` : ""}`}
      </title>
      <circle
        className="work-map__glow"
        cx={p.x}
        cy={p.y}
        r={NODE + 10}
        ref={glow}
      />
      {running ? (
        <>
          <circle className="work-map__beat" cx={p.x} cy={p.y} r={NODE} />
          {/* Two hands of a clock around the node: the inner one goes round
              in a minute, the outer one in ten. */}
          <circle
            className="work-map__hand work-map__hand--minute"
            cx={p.x}
            cy={p.y}
            pathLength={1}
            r={NODE + 4}
            transform={upright}
          />
          <circle
            className="work-map__hand work-map__hand--ten"
            cx={p.x}
            cy={p.y}
            pathLength={1}
            r={NODE + 8}
            transform={upright}
          />
        </>
      ) : null}
      <circle className="work-map__node" cx={p.x} cy={p.y} r={NODE} />
      <circle className="work-map__lit" cx={p.x} cy={p.y} r={NODE} ref={lit} />
      <path
        className="work-map__glyph"
        d={station.icon}
        ref={glyph}
        transform={`translate(${n1(p.x)} ${n1(p.y)})`}
      />
      <text className="work-map__label" ref={label} x={p.x} y={p.y + NODE + 15}>
        {station.label}
      </text>
      {tally.count > 0 ? (
        <text className="work-map__count" x={p.x} y={p.y + NODE + 28}>
          {tally.count}
        </text>
      ) : null}
      {/* Top right: how many run at once. Top left: what went wrong since
          the last message. */}
      {tally.open > 1 ? (
        <>
          <circle
            className="work-map__badge"
            cx={p.x + corner}
            cy={p.y - corner}
            r={8}
          />
          <text
            className="work-map__badge-text"
            x={p.x + corner}
            y={p.y - corner + 3}
          >
            ×{tally.open}
          </text>
        </>
      ) : null}
      {tally.faults > 0 ? (
        <>
          <circle
            className="work-map__fault"
            cx={p.x - corner}
            cy={p.y - corner}
            r={7.5}
          />
          <text
            className="work-map__fault-text"
            x={p.x - corner}
            y={p.y - corner + 3.1}
          >
            {tally.faults > 99 ? "99+" : tally.faults}
          </text>
        </>
      ) : null}
    </g>
  );
}

/**
 * The route between the stations the work went through last. The newest leg
 * draws itself; each stays for a while, then fades.
 */
function Trail({ route }: { route: WorkSummary["route"] }) {
  return route.slice(1).map((to, offset) => {
    const k = offset + 1;
    const { path, end } = leg(INDEX[route[k - 1].station], INDEX[to.station]);
    const head = k === route.length - 1;
    return (
      <g
        className="work-map__leg"
        key={`${to.station}@${to.at}`}
        style={
          {
            "--wm-leg": n1(0.12 + (0.5 * k) / (route.length - 1)),
          } as CSSProperties
        }
      >
        <path
          className={`work-map__leg-line${head ? " work-map__leg-line--head" : ""}`}
          d={path}
          pathLength={1}
        />
        {head ? (
          <circle className="work-map__leg-end" cx={end.x} cy={end.y} r={2.4} />
        ) : null}
      </g>
    );
  });
}

/**
 * The chat's work as a map: the agent in the middle, a station for each kind
 * of work around it, and a pulse along a spoke for every call and its result.
 * The picture stays in the page and only its values change, so nothing in it
 * restarts when something happens.
 */
export default function WorkMap({
  summary,
  usage,
  running,
  agentLabel,
  model,
  onAgents,
}: {
  summary: WorkSummary;
  usage: { used: number; size: number } | null;
  /** A turn is running: the core quickens. */
  running: boolean;
  agentLabel: string;
  /** The name of the model the chat is set to, when the agent offers one. */
  model: string | null;
  /** Shows the subagents; the agents station is a button while it is set. */
  onAgents?: () => void;
}) {
  const under = useRef<SVGGElement>(null);
  const over = useRef<SVGGElement>(null);
  useFlights(under, over, summary.tally);
  // The row with a long label that is shown whole; one at a time.
  const [opened, setOpened] = useState<string | null>(null);

  const full =
    usage && usage.size > 0
      ? Math.max(0, Math.min(100, Math.round((usage.used / usage.size) * 100)))
      : null;
  const described = STATIONS.filter(({ id }) => summary.tally[id].count > 0)
    .map(({ id, label }) => `${label} ${summary.tally[id].count}`)
    .join(", ");
  // The agent's first name in capitals; a long one shrinks to stay inside.
  const name = agentLabel.split(" ", 1)[0].toUpperCase();
  const nameSize = Math.min(17, 62 / (0.75 * Math.max(1, name.length)));
  const agents = LAYOUT[INDEX.agents].p;
  const crew = Math.min(6, summary.tally.agents.open);

  return (
    <div className="work-map">
      <div className="assistant-pane-picture assistant-pane-picture--fill">
        <svg
          aria-label={`Activity map. ${described || "Nothing yet"}.`}
          className={`assistant-pane-svg assistant-pane-svg--fill work-map__picture${running ? " work-map__picture--live" : ""}`}
          role="img"
          viewBox={`0 0 ${W} ${H}`}
        >
          <rect className="work-map__ground" height={H} rx={12} width={W} />
          {[
            [66, 62],
            [104, 97],
            [142, 132],
          ].map(([rx, ry]) => (
            <ellipse
              className="work-map__contour"
              cx={CX}
              cy={CY}
              key={rx}
              rx={rx}
              ry={ry}
            />
          ))}
          {/* Spokes first, then whatever flies along them, then the nodes on
              top. A spoke wears in with use, so the map shows where the work
              went. */}
          {STATIONS.map((station, index) => {
            const { hub, rim } = LAYOUT[index];
            const worn = Math.min(
              1,
              Math.log2(1 + summary.tally[station.id].count) / 7,
            );
            const line = {
              strokeWidth: n1(1 + 2.6 * worn),
              x1: hub.x,
              x2: rim.x,
              y1: hub.y,
              y2: rim.y,
            };
            return (
              <g key={station.id}>
                <line className="work-map__spoke" {...line} />
                {worn > 0 ? (
                  <line
                    className={`work-map__spoke work-map__spoke--worn${station.id === "memory" ? " work-map__spoke--memory" : ""}`}
                    opacity={n1(worn * 0.55)}
                    {...line}
                  />
                ) : null}
              </g>
            );
          })}
          <Trail route={summary.route} />
          <g ref={under} />
          {STATIONS.map((station, index) => (
            <Station
              index={index}
              key={station.id}
              station={station}
              tally={summary.tally[station.id]}
            />
          ))}
          {/* Each subagent still at work circles the agents station. */}
          {Array.from({ length: crew }, (_, k) => (
            <g
              className="work-map__crew"
              key={k}
              style={{
                animationDelay: `${-Math.round((k * 7000) / crew)}ms`,
                transformOrigin: `${n1(agents.x)}px ${n1(agents.y)}px`,
              }}
            >
              <circle cx={agents.x} cy={agents.y - NODE - 6} r={3} />
            </g>
          ))}
          <circle className="work-map__breath" cx={CX} cy={CY} r={CORE + 3} />
          <circle className="work-map__core" cx={CX} cy={CY} r={CORE} />
          {/* The context window as a ring around the core. */}
          {full === null ? null : (
            <>
              <circle
                className="work-map__gauge-track"
                cx={CX}
                cy={CY}
                r={CORE + 5}
              />
              <circle
                className={`work-map__gauge${full >= 90 ? " work-map__gauge--full" : full >= 70 ? " work-map__gauge--high" : ""}`}
                cx={CX}
                cy={CY}
                pathLength={100}
                r={CORE + 5}
                strokeDasharray={`${full} 100`}
                transform={`rotate(-90 ${CX} ${CY})`}
              />
              <text className="work-map__percent" x={CX} y={CY - 19}>
                {full}%
              </text>
            </>
          )}
          <text
            className="work-map__name"
            style={{ "--wm-name-size": n1(nameSize) } as CSSProperties}
            x={CX}
            y={CY + (model === null ? 6 : 1)}
          >
            {name}
          </text>
          {model === null ? null : (
            <text className="work-map__model" x={CX} y={CY + 17}>
              {model.toLowerCase()}
            </text>
          )}
          <g ref={over} />
        </svg>
        {/* The picture keeps its proportions, so a button laid over the
            agents station in shares of the picture stays on it. */}
        {onAgents ? (
          <AppButton
            aria-label="Show the agents"
            className="work-map__station-button"
            color="inherit"
            onClick={onAgents}
            style={{
              left: `${n1(((agents.x - NODE - 6) / W) * 100)}%`,
              top: `${n1(((agents.y - NODE - 6) / H) * 100)}%`,
              width: `${n1(((2 * NODE + 12) / W) * 100)}%`,
              // Down over its name and count too.
              height: `${n1(((2 * NODE + 38) / H) * 100)}%`,
            }}
            title="Show the agents"
          />
        ) : null}
      </div>
      {/* Only what runs: the context and the model are in the picture. */}
      <p
        className={`work-map__now ${ROW_TEXT}`}
        title={summary.now ?? undefined}
      >
        {summary.now ?? "Nothing is running."}
      </p>
      {summary.rows.length === 0 ? (
        <p className="work-map__now assistant-pane-text assistant-pane-text--dim">
          Nothing yet.
        </p>
      ) : (
        <ol className="work-map__feed">
          {/* One line a row: the words are cut at the card's edge and the
              time keeps a column at the right, so a row neither wraps nor
              moves while the work goes on. */}
          {summary.rows.map((row) => {
            const long = row.label.length > LONG_LABEL;
            const whole = long && opened === row.id;
            return (
              <li
                className={`work-map__row ${ROW_TEXT} assistant-pane-text--${ROW_TONE[row.status]}`}
                key={row.id}
              >
                <span
                  className={`work-map__row-text${whole ? " work-map__row-text--whole" : ""}`}
                >
                  {rowLine(row)}
                </span>
                {long ? (
                  <AppButton
                    aria-expanded={whole}
                    aria-label="Show the whole row"
                    className="work-map__row-open"
                    color="inherit"
                    onClick={() => setOpened(whole ? null : row.id)}
                    title={whole ? "Shorten the row" : "Show the whole row"}
                  >
                    {whole ? "▾" : "▸"}
                  </AppButton>
                ) : null}
                <span className="work-map__row-time">{rowTime(row)}</span>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
