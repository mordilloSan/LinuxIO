// src/utils/filebrowser.ts
import axios from "@/utils/axios";

export const FB_IFRAME_ID = "filebrowser-iframe";
export const FB_LAYER_ID = "filebrowser-layer";
export const FB_BASE = "/navigator";

const API = `${FB_BASE}/api/users`;

function getFbIframe(): HTMLIFrameElement | null {
  return document.getElementById(FB_IFRAME_ID) as HTMLIFrameElement | null;
}

function getFbDoc(): Document | null {
  const iframe = getFbIframe();
  return iframe?.contentDocument || iframe?.contentWindow?.document || null;
}

// ---------- FB user + prefs ----------
type Prefs = Record<string, unknown>;

let cachedId: number | undefined;
let inflight: Promise<number> | null = null;

export function resetFilebrowserUserCache(): void {
  cachedId = undefined;
  inflight = null;
}

export async function resolveFilebrowserUserId(): Promise<number> {
  if (typeof cachedId === "number") return cachedId;
  if (inflight) return inflight;

  inflight = (async () => {
    const controller = new AbortController();
    const t = setTimeout(() => controller.abort(), 1000);

    try {
      const r = await axios.get(API, {
        params: { id: "self" },
        signal: controller.signal,
      });
      const u = Array.isArray(r.data) ? r.data[0] : r.data;
      const id = Number(u?.id);
      if (!Number.isFinite(id)) throw new Error("FB user id not found");
      cachedId = id;
      return id;
    } finally {
      clearTimeout(t);
    }
  })();

  try {
    return await inflight;
  } finally {
    inflight = null;
  }
}

export async function updateUserPrefs(prefs: Prefs): Promise<void> {
  const id = await resolveFilebrowserUserId();
  const payload = { what: "user", which: Object.keys(prefs), data: prefs };
  await axios.put(API, payload, { params: { id } });
}

export const setFilebrowserDarkMode = (dark: boolean) =>
  updateUserPrefs({ darkMode: dark });

export const setFilebrowserThemeColor = (token?: string) =>
  updateUserPrefs({ themeColor: token ? `var(--icon-${token})` : "" });

// ---------- Live preview ----------
export function liveSetPrimaryToken(token?: string): boolean {
  const doc = getFbDoc();
  if (!doc) return false;

  const root = doc.documentElement;
  if (token) {
    root.style.setProperty("--primaryColor", `var(--icon-${token})`);
  } else {
    root.style.removeProperty("--primaryColor");
  }
  return true;
}

/** Live set dark mode (no persistence): scoped observers for sidebar/overlay only. */
export function liveSetDarkMode(wantDark: boolean): boolean {
  const doc = getFbDoc();
  if (!doc) return false;
  ensureReactiveDarkMode(doc, wantDark);
  return true;
}

/* ---------------- internal: reactive dark-mode controller (scoped) ---------------- */

type DarkState = {
  wantDark: boolean;
  treeObs?: MutationObserver;
  sidebarObs?: MutationObserver;
  overlayObs?: MutationObserver;
  sidebarEl?: Element | null;
  overlayEl?: Element | null;
  raf?: number;
};

const darkStates = new WeakMap<Document, DarkState>();

function ensureReactiveDarkMode(doc: Document, wantDark: boolean) {
  // remember target state
  const state = darkStates.get(doc) ?? { wantDark };
  state.wantDark = wantDark;
  darkStates.set(doc, state);

  // apply immediately to whatever exists
  applyDarkNow(doc, wantDark);

  // (re)bind attribute observers to specific nodes
  bindAttrObservers(doc, state);

  // single subtree observer: only childList (no attributes) to notice mounts
  if (!state.treeObs) {
    const obs = new MutationObserver(() => {
      // throttle to next frame
      if (state.raf) cancelAnimationFrame(state.raf);
      state.raf = requestAnimationFrame(() => {
        const s = darkStates.get(doc);
        if (!s) return;
        // elements may have mounted/unmounted — rebind attr observers
        bindAttrObservers(doc, s);
        // and re-apply to any new nodes
        applyDarkNow(doc, s.wantDark);
      });
    });

    obs.observe(doc.body || doc, { childList: true, subtree: true });
    state.treeObs = obs;
  }
}

/** Attach attribute observers only to the nodes that matter (sidebar & overlay). */
function bindAttrObservers(doc: Document, state: DarkState) {
  const sidebar = doc.querySelector("#sidebar");
  const overlay = doc.querySelector(".overlay");

  // rebind sidebar observer if node changed
  if (sidebar !== state.sidebarEl) {
    state.sidebarObs?.disconnect();
    state.sidebarEl = sidebar || null;

    if (sidebar) {
      const so = new MutationObserver(() => {
        applyDarkNow(doc, state.wantDark);
      });
      so.observe(sidebar, { attributes: true, attributeFilter: ["class", "style"] });
      state.sidebarObs = so;
    } else {
      state.sidebarObs = undefined;
    }
  }

  // rebind overlay observer if node changed
  if (overlay !== state.overlayEl) {
    state.overlayObs?.disconnect();
    state.overlayEl = overlay || null;

    if (overlay) {
      const oo = new MutationObserver(() => {
        applyDarkNow(doc, state.wantDark);
      });
      oo.observe(overlay, { attributes: true, attributeFilter: ["class", "style"] });
      state.overlayObs = oo;
    } else {
      state.overlayObs = undefined;
    }
  }
}

/** Freshly query and toggle classes so late-mounted or class-reset nodes are updated. */
function applyDarkNow(doc: Document, dark: boolean): void {
  const header = doc.querySelector<HTMLElement>("header.flexbar");
  const sidebar = doc.querySelector<HTMLElement>("nav#sidebar");
  const overlay = doc.querySelector<HTMLElement>(".overlay");
  const scroll = doc.querySelector<HTMLElement>(".scroll-wrapper");
  const headerCards = Array.from(
    doc.querySelectorAll<HTMLElement>(".header.card"),
  );

  header?.classList.toggle("dark-mode-header", dark);
  sidebar?.classList.toggle("dark-mode", dark);
  scroll?.classList.toggle("dark-mode", dark);

  headerCards.forEach((el) => el.classList.toggle("dark-mode-item-header", dark));
}
