import type { AppConfig } from "@/types/config";

const CONFIG_CACHE_PREFIX = "linuxio_config:";
const CONFIG_CACHE_VERSION = 1;

type ConfigCacheEntry = {
  version: number;
  config: Partial<AppConfig>;
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);

const getSessionStorage = (): Storage | null => {
  if (typeof window === "undefined") return null;
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
};

const keyForUser = (username: string) => `${CONFIG_CACHE_PREFIX}${username}`;

export function readConfigCache(
  username: string | null | undefined,
): Partial<AppConfig> | null {
  if (!username) return null;
  const storage = getSessionStorage();
  if (!storage) return null;

  try {
    const raw = storage.getItem(keyForUser(username));
    if (!raw) return null;

    const parsed = JSON.parse(raw) as unknown;
    if (
      !isRecord(parsed) ||
      parsed.version !== CONFIG_CACHE_VERSION ||
      !isRecord(parsed.config)
    ) {
      return null;
    }

    return parsed.config as Partial<AppConfig>;
  } catch {
    return null;
  }
}

export function writeConfigCache(
  username: string | null | undefined,
  config: AppConfig,
): void {
  if (!username) return;
  const storage = getSessionStorage();
  if (!storage) return;

  const entry: ConfigCacheEntry = {
    version: CONFIG_CACHE_VERSION,
    config,
  };

  try {
    storage.setItem(keyForUser(username), JSON.stringify(entry));
  } catch {
    // Best-effort cache only.
  }
}

export function clearConfigCache(): void {
  const storage = getSessionStorage();
  if (!storage) return;

  try {
    for (let i = storage.length - 1; i >= 0; i -= 1) {
      const key = storage.key(i);
      if (key?.startsWith(CONFIG_CACHE_PREFIX)) {
        storage.removeItem(key);
      }
    }
  } catch {
    // Best-effort cleanup only.
  }
}
