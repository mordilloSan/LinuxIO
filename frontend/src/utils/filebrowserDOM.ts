// src/utils/filebrowserDOM.ts
import { normalizeToken } from "@/theme/colors";

/** Is the FileBrowser layer currently visible? */
export function isFBVisible(): boolean {
  const el = document.getElementById("filebrowser-layer");
  if (!el) return false;
  const cs = getComputedStyle(el);
  return cs.visibility === "visible" && parseFloat(cs.opacity || "0") > 0.5;
}

/** Get the FileBrowser iframe's document (same-origin). */
export function getFBDoc(): Document | null {
  const iframe = document.getElementById(
    "filebrowser-iframe",
  ) as HTMLIFrameElement | null;
  return iframe?.contentDocument || iframe?.contentWindow?.document || null;
}

/** Hidden reload when FB layer is not visible. */
export function bgReloadFBIfHidden(): void {
  const iframe = document.getElementById(
    "filebrowser-iframe",
  ) as HTMLIFrameElement | null;
  if (!iframe || isFBVisible()) return;
  try {
    iframe.contentWindow?.location?.reload();
  } catch {
    const current = iframe.src;
    iframe.src = "about:blank";
    setTimeout(() => {
      iframe.src = current;
    }, 0);
  }
}

/**
 * Live-apply primary token inside FB:
 *   <html style="--primaryColor: var(--icon-<token>)">
 */
export function setFBPrimaryToken(token?: string): boolean {
  const valid = normalizeToken(token);
  const doc = getFBDoc();
  if (!doc) return false;

  doc.documentElement.style.setProperty(
    "--primaryColor",
    `var(--icon-${valid})`,
  );

  // Optional: reflect active state if FB shows color buttons
  const group = doc.querySelector(".button-group");
  if (group) {
    group.querySelectorAll("button").forEach((btn) => {
      const b = btn as HTMLButtonElement;
      const text = b.textContent?.trim().toLowerCase();
      b.classList.toggle("active", text === valid);
    });
  }
  return true;
}

/**
 * Live-apply dark mode inside FB.
 * Tries (1) data-theme attr, (2) 'dark' class, (3) clicking quick-toggle icon.
 */
export function setFBDarkMode(dark: boolean): boolean {
  const doc = getFBDoc();
  if (!doc) return false;

  // Attempt 1: attribute / class toggles (harmless if unused by FB)
  doc.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
  doc.documentElement.classList.toggle("dark", dark);

  // Attempt 2: click the visible toggle icon if present
  const icons = Array.from(
    doc.querySelectorAll(".quick-toggles .material-icons"),
  );
  const want = dark ? "dark_mode" : "light_mode";
  const target =
    icons.find((el) => el.textContent?.trim() === want) ||
    icons.find((el) =>
      ["dark_mode", "light_mode"].includes(el.textContent?.trim() || ""),
    );

  if (target) {
    (target as HTMLElement).click();
    return true;
  }
  return true; // we set attributes/classes; even if FB ignores them, this is best-effort.
}
