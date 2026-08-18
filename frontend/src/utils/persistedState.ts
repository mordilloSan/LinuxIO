/**
 * localStorage-backed persistence for per-browser UI state — view toggles,
 * expanded rows — that should survive a reload but is not worth a server
 * config round-trip. Storage can be unavailable (private mode) or hold a
 * stale/corrupt value from an older build, so reads validate before trusting
 * and every failure degrades to "no stored value".
 */

export function readPersistedState<T>(
  key: string,
  isValid: (value: unknown) => value is T,
): T | undefined {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === null) return undefined;
    const parsed: unknown = JSON.parse(raw);
    return isValid(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

export function writePersistedState(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Best-effort: the state simply starts from its default next load.
  }
}
