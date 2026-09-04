/** Helpers for Go-style duration strings ("30m", "1h30m", "1m0s", "0"). */

const GO_DURATION_PART_PATTERN = /(-?\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g;

/** Drop zero-valued parts from a Go duration ("1m0s" -> "1m"). Returns the input unchanged when it is not a valid duration. */
export const compactGoDuration = (value: string) => {
  const trimmed = value.trim();
  if (trimmed === "0") return "0";

  const parts: string[] = [];
  let index = 0;
  let matched = false;

  for (const match of trimmed.matchAll(GO_DURATION_PART_PATTERN)) {
    matched = true;
    if (match.index !== index) return trimmed;
    index = match.index + match[0].length;
    if (Number(match[1]) !== 0) {
      parts.push(match[0]);
    }
  }

  if (!matched || index !== trimmed.length) return trimmed;
  return parts.length > 0 ? parts.join("") : "0";
};

/** True for a non-negative Go duration string (or "0"). */
export const isGoDuration = (value: string) => {
  const trimmed = value.trim();
  if (trimmed === "0") return true;

  let index = 0;
  let matched = false;
  for (const match of trimmed.matchAll(GO_DURATION_PART_PATTERN)) {
    const matchIndex = match.index ?? -1;
    matched = true;
    if (matchIndex !== index || Number(match[1]) < 0) return false;
    index = matchIndex + match[0].length;
  }

  return matched && index === trimmed.length;
};

const GO_DURATION_UNIT_MS: Record<string, number> = {
  ns: 1e-6,
  us: 1e-3,
  µs: 1e-3,
  μs: 1e-3,
  ms: 1,
  s: 1_000,
  m: 60_000,
  h: 3_600_000,
};

/** Milliseconds for a non-negative Go duration string, or null when it is not one. */
export const goDurationToMs = (value: string) => {
  if (!isGoDuration(value)) return null;
  let total = 0;
  for (const match of value.trim().matchAll(GO_DURATION_PART_PATTERN)) {
    total += Number(match[1]) * GO_DURATION_UNIT_MS[match[2]];
  }
  return total;
};
