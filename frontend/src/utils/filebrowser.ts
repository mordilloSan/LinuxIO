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
/* ---------------- sidebar-only reactive dark-mode controller ---------------- */

type DarkState = {
  wantDark: boolean;
  treeObs?: MutationObserver;
  sidebarObs?: MutationObserver;
  sidebarEl?: Element | null;
  raf?: number;
};

const darkStates = new WeakMap<Document, DarkState>();

export function liveSetDarkMode(wantDark: boolean): boolean {
  const doc = getFbDoc();
  if (!doc) return false;
  ensureSidebarReactiveDarkMode(doc, wantDark);
  const header = doc.querySelector<HTMLElement>("header.flexbar");
  const scroll = doc.querySelector<HTMLElement>(".scroll-wrapper");
  const headerCards = Array.from(
    doc.querySelectorAll<HTMLElement>(".header.card"),
  );

  header?.classList.toggle("dark-mode-header", wantDark);
  scroll?.classList.toggle("dark-mode", wantDark);
  headerCards.forEach((el) =>
    el.classList.toggle("dark-mode-item-header", wantDark),
  );
  return true;
}

function ensureSidebarReactiveDarkMode(doc: Document, wantDark: boolean) {
  // remember target state per document
  const state = darkStates.get(doc) ?? { wantDark };
  state.wantDark = wantDark;
  darkStates.set(doc, state);

  // apply immediately to whatever exists now
  applySidebarDark(doc, wantDark);

  // (re)bind attribute observer to the sidebar only
  bindSidebarObserver(doc, state);

  // one subtree observer: watch for sidebar being remounted/replaced
  if (!state.treeObs) {
    const obs = new MutationObserver(() => {
      if (state.raf) cancelAnimationFrame(state.raf);
      state.raf = requestAnimationFrame(() => {
        const s = darkStates.get(doc);
        if (!s) return;
        bindSidebarObserver(doc, s); // rebind if node changed
        applySidebarDark(doc, s.wantDark); // re-apply class to fresh node
      });
    });

    if (doc.body) {
      obs.observe(doc.body, { childList: true, subtree: true });
    }
    state.treeObs = obs;
  }
}

/** Attach an attribute observer to the current sidebar node (if present). */
function bindSidebarObserver(doc: Document, state: DarkState) {
  const sidebar = doc.querySelector("nav#sidebar");

  // only (re)bind if the node identity changed
  if (sidebar !== state.sidebarEl) {
    state.sidebarObs?.disconnect();
    state.sidebarEl = sidebar || null;

    if (sidebar) {
      const so = new MutationObserver(() => {
        // if FB resets classes/styles, put ours back
        applySidebarDark(doc, state.wantDark);
      });
      so.observe(sidebar, {
        attributes: true,
        attributeFilter: ["class", "style"],
      });
      state.sidebarObs = so;
    } else {
      state.sidebarObs = undefined;
    }
  }
}

/** Toggle only the sidebar’s dark class. */
function applySidebarDark(doc: Document, dark: boolean): void {
  const sidebar = doc.querySelector<HTMLElement>("nav#sidebar");
  sidebar?.classList.toggle("dark-mode", dark);
}
