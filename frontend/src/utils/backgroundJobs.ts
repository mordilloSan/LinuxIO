export function jobIdentityKey(type: string, request: unknown = {}) {
  return JSON.stringify([type, request ?? {}]);
}

/** A job snapshot's untyped request payload as an indexable object. */
export function requestObject(request: unknown): Record<string, unknown> {
  return request && typeof request === "object"
    ? (request as Record<string, unknown>)
    : {};
}

export function requestString(
  request: Record<string, unknown>,
  key: string,
): string | undefined {
  const value = request[key];
  return typeof value === "string" ? value : undefined;
}

export function makeCountedSet() {
  const counts = new Map<string, number>();
  return {
    add(key: string) {
      counts.set(key, (counts.get(key) ?? 0) + 1);
    },
    delete(key: string) {
      const n = counts.get(key) ?? 0;
      if (n <= 1) counts.delete(key);
      else counts.set(key, n - 1);
    },
    has(key: string) {
      return counts.has(key);
    },
  };
}

export type CountedSet = ReturnType<typeof makeCountedSet>;

export function createProgressSpeedCalculator(minWindowMs = 500, alpha = 0.3) {
  let lastBytes = 0;
  let lastTime = Date.now();
  let ewmaSpeed: number | undefined;

  return (bytes: number): number | undefined => {
    const now = Date.now();
    const deltaBytes = bytes - lastBytes;
    const deltaMs = now - lastTime;

    if (deltaMs > minWindowMs && deltaBytes > 0) {
      lastBytes = bytes;
      lastTime = now;
      const instantSpeed = deltaBytes / (deltaMs / 1000);
      ewmaSpeed =
        ewmaSpeed === undefined
          ? instantSpeed
          : alpha * instantSpeed + (1 - alpha) * ewmaSpeed;
      return ewmaSpeed;
    }

    return undefined;
  };
}
