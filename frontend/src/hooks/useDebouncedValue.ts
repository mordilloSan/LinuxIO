import { useEffect, useState } from "react";

/**
 * Returns `value` once it has stayed unchanged for `waitMs`. Every change
 * restarts the timer, so a burst of updates settles into a single re-render.
 */
export function useDebouncedValue<T>(value: T, waitMs: number): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timeout = setTimeout(() => setDebounced(value), waitMs);
    return () => clearTimeout(timeout);
  }, [value, waitMs]);

  return debounced;
}
