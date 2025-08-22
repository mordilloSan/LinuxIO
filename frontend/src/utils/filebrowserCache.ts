// src/utils/filebrowserCache.ts
import axios from "@/utils/axios";
import { normalizeToken } from "@/theme/colors";

type Prefs = Record<string, any>;
const API = "/navigator/api/users";

enum Mode {
  Unknown,
  NoIdOK,
  SelfOK,
  NeedsId,
}
let mode: Mode = Mode.Unknown;

// cache: undefined = not resolved yet
let cachedId: number | undefined;
let resolvingId: Promise<number> | null = null;

async function getCurrentUsername(): Promise<string> {
  const r = await axios.get("/auth/me");
  const name = r?.data?.user?.name || r?.data?.user?.id || "";
  if (!name) throw new Error("missing username from /auth/me");
  return name;
}

/** Resolve the current FileBrowser user's numeric id (never null). */
export async function resolveFilebrowserUserId(): Promise<number> {
  if (typeof cachedId === "number") return cachedId;
  if (resolvingId) return resolvingId;

  resolvingId = (async () => {
    // 1) Try GET ?id=self (preferred if supported)
    try {
      const r = await axios.get(API, { params: { id: "self" } });
      const u = Array.isArray(r.data) ? r.data[0] : r.data;
      const selfId = Number(u?.id);
      if (Number.isFinite(selfId)) {
        cachedId = selfId;
        return cachedId!;
      }
    } catch {
      /* continue */
    }

    // 2) Fallback: list users and match by username
    try {
      const username = await getCurrentUsername();
      const r = await axios.get(API);
      const list: any[] = Array.isArray(r.data)
        ? r.data
        : Array.isArray(r.data?.data)
          ? r.data.data
          : [];
      const hit = list.find((x) => x?.username === username);
      const listId = Number(hit?.id);
      if (Number.isFinite(listId)) {
        cachedId = listId;
        return cachedId!;
      }
    } catch {
      /* ignore */
    }

    // 3) Last resort (single-user default)
    cachedId = 1;
    return cachedId!;
  })();

  try {
    return await resolvingId;
  } finally {
    resolvingId = null;
  }
}

/** Update current user's prefs; tries no-id, then id=self, then numeric id. */
export async function updateUserPrefs(prefs: Prefs): Promise<void> {
  const payload = { what: "user", which: Object.keys(prefs), data: prefs };

  // A) Try no-id once; lock if it works
  if (mode !== Mode.NeedsId && mode !== Mode.SelfOK) {
    try {
      const r = await axios.put(API, payload);
      if (r.status >= 200 && r.status < 300) {
        mode = Mode.NoIdOK;
        return;
      }
    } catch (e: any) {
      const st = e?.response?.status;
      if ([400, 401, 403, 404, 405].includes(st)) {
        mode = Mode.SelfOK;
      } else {
        throw e;
      }
    }
  }

  // B) Try id=self
  if (mode === Mode.SelfOK) {
    try {
      const r = await axios.put(API, payload, { params: { id: "self" } });
      if (r.status >= 200 && r.status < 300) return;
      mode = Mode.NeedsId;
    } catch (e: any) {
      const st = e?.response?.status;
      if ([400, 401, 403, 404, 405].includes(st)) {
        mode = Mode.NeedsId;
      } else {
        throw e;
      }
    }
  }

  // C) Resolve numeric id and use it
  const id = await resolveFilebrowserUserId();
  await axios.put(API, payload, { params: { id } });
}

/** Map our token -> FileBrowser CSS var like: var(--icon-blue) */
function tokenToFbVar(token?: string): string {
  const normalized = normalizeToken(token);
  return `var(--icon-${normalized})`;
}

/** Convenience wrappers (existing ones kept as-is) */
export const setDarkMode = (dark: boolean) =>
  updateUserPrefs({ darkMode: dark });

export const setShowHidden = (show: boolean) =>
  updateUserPrefs({ showHidden: show });

export const setViewMode = (mode: "normal" | "gallery" | "list" | string) =>
  updateUserPrefs({ viewMode: mode });

/** NEW: set primary color token for FileBrowser */
export const setThemeColor = (tokenName?: string) =>
  updateUserPrefs({
    themeColor: tokenToFbVar(tokenName),
  });
