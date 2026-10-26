/** Deterministic hue for a log source name so its badge colour is stable across sessions. */
export function logSourceHue(name: string): number {
  let hash = 0;
  for (const char of name) {
    hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0;
  }
  return hash % 360;
}
