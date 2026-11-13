import type { PaneView } from "@/api/acp";

/** What a pane is called in the panel and its buttons. */
export const paneTitle = (pane: Pick<PaneView, "title" | "plugin">) =>
  pane.title || pane.plugin || "Plugin pane";

/**
 * What a plugin's render tree may draw, after every field has been checked.
 * Strings stay strings (React renders them as text), an Svg becomes a data
 * URI for an <img>, and a Link keeps only an http(s) href.
 */
export type PaneItem =
  | string
  | { kind: "box"; className: string; children: PaneItem[] }
  | { kind: "text"; className: string; children: PaneItem[] }
  | {
      kind: "svg";
      src: string;
      alt: string;
      width?: number;
      height?: number;
    }
  | { kind: "markdown"; text: string; dim: boolean }
  | { kind: "code"; source: string }
  | { kind: "link"; href: string | null; children: PaneItem[] };

export interface PaneTreeRead {
  items: PaneItem[];
  /** A Button, Input, Select or unknown node was left out. */
  unsupported: boolean;
  /** A limit was reached and the rest of the tree was left out. */
  truncated: boolean;
}

/**
 * Bounds on one tree, so a hostile plugin cannot hang the page. The node and
 * depth limits match Claude Code's own renderer; the character budget counts
 * every string, SVG sources included.
 */
export const PANE_TREE_LIMITS = {
  depth: 32,
  nodes: 2000,
  chars: 2 * 1024 * 1024,
};

const MAX_HREF = 2048;
const MAX_PICTURE_SIDE = 4096;

interface Walk {
  nodes: number;
  chars: number;
  /** The node or character budget is spent: nothing more is read. */
  full: boolean;
  unsupported: boolean;
  truncated: boolean;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const pick = <T extends string>(
  value: unknown,
  table: Record<string, T>,
): T | undefined =>
  typeof value === "string" && Object.hasOwn(table, value)
    ? table[value]
    : undefined;

// Box layout props are terminal cells; 1, 2 and 3+ cells map to space steps.
function step(value: unknown): 1 | 2 | 3 | undefined {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 1) {
    return undefined;
  }
  return value >= 3 ? 3 : value >= 2 ? 2 : 1;
}

const DIRECTIONS: Record<string, string> = {
  column: "column",
  "row-reverse": "row-reverse",
  "column-reverse": "column-reverse",
};
const WRAPS: Record<string, string> = {
  wrap: "wrap",
  nowrap: "nowrap",
  "wrap-reverse": "wrap-reverse",
};
const ALIGNS: Record<string, string> = {
  "flex-start": "start",
  center: "center",
  "flex-end": "end",
  stretch: "stretch",
};
const JUSTIFIES: Record<string, string> = {
  "flex-start": "start",
  center: "center",
  "flex-end": "end",
  "space-between": "between",
  "space-around": "around",
  "space-evenly": "evenly",
};

/** CSS classes for a Box's layout props; values that do not map are ignored. */
function boxClass(props: Record<string, unknown>): string {
  const base = "assistant-pane-box";
  const classes = [base];
  const add = (modifier: string | number | undefined, prefix = "") => {
    if (modifier !== undefined) classes.push(`${base}--${prefix}${modifier}`);
  };
  add(pick(props.flexDirection, DIRECTIONS));
  add(pick(props.flexWrap, WRAPS));
  add(pick(props.alignItems, ALIGNS), "align-");
  add(pick(props.justifyContent, JUSTIFIES), "justify-");
  add(step(props.gap), "gap-");
  add(step(props.padding), "pad-");
  add(step(props.paddingX), "pad-x-");
  add(step(props.paddingY), "pad-y-");
  if (typeof props.borderStyle === "string") {
    add("border");
    if (props.borderStyle === "round") add("round");
  }
  if (typeof props.flexGrow === "number" && props.flexGrow > 0) add("grow");
  return classes.join(" ");
}

// Claude Code's semantic colour names and the ANSI names (with an optional
// "ansi:" prefix and "Bright" suffix), mapped to theme tones. Hex and rgb()
// values are ignored: plugin content does not pick LinuxIO's colours.
const TONES: Record<string, string> = {
  inactive: "secondary",
  subtle: "secondary",
  secondaryText: "secondary",
  gray: "secondary",
  grey: "secondary",
  black: "secondary",
  error: "error",
  red: "error",
  diffRemoved: "error",
  success: "success",
  green: "success",
  diffAdded: "success",
  warning: "warning",
  yellow: "warning",
  blue: "info",
  cyan: "info",
  claude: "accent",
  permission: "accent",
  suggestion: "accent",
  ide: "accent",
  magenta: "accent",
};

function tone(color: unknown): string | undefined {
  if (typeof color !== "string") return undefined;
  return pick(color.replace(/^ansi:/, "").replace(/Bright$/, ""), TONES);
}

// Text sizes are Apple text-style names ("footnote" in the wild).
const SIZES: Record<string, string> = {
  caption: "small",
  caption2: "small",
  footnote: "small",
  headline: "large",
  title: "large",
  title2: "large",
  title3: "large",
  largeTitle: "large",
};

/** CSS classes for a Text's style props; values that do not map are ignored. */
function textClass(props: Record<string, unknown>): string {
  const base = "assistant-pane-text";
  const classes = [base];
  const flag = (prop: string, modifier: string) => {
    if (props[prop] === true) classes.push(`${base}--${modifier}`);
  };
  flag("bold", "bold");
  flag("italic", "italic");
  flag("underline", "underline");
  flag("strikethrough", "strike");
  flag("dimColor", "dim");
  flag("mono", "mono");
  const size = pick(props.size, SIZES);
  if (size) classes.push(`${base}--${size}`);
  const color = tone(props.color);
  if (color) classes.push(`${base}--${color}`);
  return classes.join(" ");
}

/** Only http and https, without credentials; anything else is not a link. */
export function safeHref(value: unknown): string | null {
  if (typeof value !== "string" || value.length > MAX_HREF) return null;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  if (url.username || url.password) return null;
  return url.href;
}

const pictureSide = (value: unknown) =>
  typeof value === "number" && value > 0 && value <= MAX_PICTURE_SIDE
    ? value
    : undefined;

// Counts one node and its characters against the limits.
function take(walk: Walk, chars: number): boolean {
  walk.nodes += 1;
  walk.chars += chars;
  if (
    walk.nodes > PANE_TREE_LIMITS.nodes ||
    walk.chars > PANE_TREE_LIMITS.chars
  ) {
    walk.full = true;
    walk.truncated = true;
    return false;
  }
  return true;
}

function readChildren(
  children: unknown,
  depth: number,
  walk: Walk,
): PaneItem[] {
  const list = Array.isArray(children) ? children : [children];
  const items: PaneItem[] = [];
  for (const child of list) {
    if (walk.full) break;
    items.push(...readNode(child, depth, walk));
  }
  return items;
}

function readNode(value: unknown, depth: number, walk: Walk): PaneItem[] {
  if (typeof value === "string" || typeof value === "number") {
    const text = String(value);
    return take(walk, text.length) ? [text] : [];
  }
  // null, booleans and anything without a type draw nothing, as in React.
  if (!isRecord(value) || typeof value.type !== "string") return [];
  if (depth >= PANE_TREE_LIMITS.depth) {
    walk.truncated = true;
    return [];
  }
  const props = isRecord(value.props) ? value.props : {};
  const text = (prop: unknown) => (typeof prop === "string" ? prop : null);
  const children = () => readChildren(value.children, depth + 1, walk);
  switch (value.type) {
    case "Box":
    case "div":
      if (!take(walk, 0) || props.display === "none") return [];
      return [
        { kind: "box", className: boxClass(props), children: children() },
      ];
    case "Text":
    case "span":
    case "b": {
      if (!take(walk, 0)) return [];
      const style = value.type === "b" ? { ...props, bold: true } : props;
      return [
        { kind: "text", className: textClass(style), children: children() },
      ];
    }
    case "Svg": {
      const source = text(props.source);
      if (source === null || !take(walk, source.length)) return [];
      return [
        {
          kind: "svg",
          src: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(source)}`,
          alt: text(props.alt) ?? "",
          width: pictureSide(props.width),
          height: pictureSide(props.height),
        },
      ];
    }
    case "Markdown": {
      const markdown = text(props.text);
      if (markdown === null || !take(walk, markdown.length)) return [];
      return [
        { kind: "markdown", text: markdown, dim: props.dimColor === true },
      ];
    }
    case "Code": {
      const source = text(props.source);
      if (source === null || !take(walk, source.length)) return [];
      return [{ kind: "code", source }];
    }
    case "Link": {
      const raw = text(props.href);
      const label = text(props.label);
      if (!take(walk, (raw?.length ?? 0) + (label?.length ?? 0))) return [];
      const inner = children();
      const fallback = label ?? raw;
      return [
        {
          kind: "link",
          href: safeHref(raw),
          children: inner.length > 0 || fallback === null ? inner : [fallback],
        },
      ];
    }
    case "Button":
    case "Input":
    case "Select":
      walk.unsupported = true;
      return [];
    default:
      // An unknown container: its own drawing is left out, its children stay.
      walk.unsupported = true;
      return take(walk, 0) ? children() : [];
  }
}

/** Checks a plugin's render tree and turns it into what the pane may draw. */
export function readPaneTree(tree: unknown): PaneTreeRead {
  const walk: Walk = {
    nodes: 0,
    chars: 0,
    full: false,
    unsupported: false,
    truncated: false,
  };
  const items = readNode(tree, 0, walk);
  return {
    items,
    unsupported: walk.unsupported,
    truncated: walk.truncated,
  };
}
