const TAB_NAVIGATION_ATTRIBUTE = "data-tab-navigation";

let installationCount = 0;
let teardownInstalled: (() => void) | undefined;

export function isTabNavigationActive(): boolean {
  return (
    typeof document !== "undefined" &&
    document.documentElement.hasAttribute(TAB_NAVIGATION_ATTRIBUTE)
  );
}

/**
 * Tracks whether the user is currently navigating by Tab and exposes that
 * state on the document root for focus-ring styling.
 *
 * Focus itself is intentionally left untouched: this only controls the
 * presentation intent used by CSS. The installer is safe to call during SSR
 * and is reference-counted so StrictMode/tests cannot register duplicate
 * listeners.
 */
export function installTabNavigationIntent(): () => void {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return () => undefined;
  }

  installationCount += 1;
  if (teardownInstalled) {
    return createRelease();
  }

  const root = document.documentElement;
  const disable = () => root.removeAttribute(TAB_NAVIGATION_ATTRIBUTE);
  const enableOnTab = (event: KeyboardEvent) => {
    if (event.key === "Tab") {
      root.setAttribute(TAB_NAVIGATION_ATTRIBUTE, "true");
    }
  };
  function handleVisibilityChange() {
    if (document.hidden) disable();
  }

  window.addEventListener("keydown", enableOnTab, true);
  window.addEventListener("pointerdown", disable, true);
  window.addEventListener("blur", disable);
  document.addEventListener("visibilitychange", handleVisibilityChange);

  teardownInstalled = () => {
    window.removeEventListener("keydown", enableOnTab, true);
    window.removeEventListener("pointerdown", disable, true);
    window.removeEventListener("blur", disable);
    document.removeEventListener("visibilitychange", handleVisibilityChange);
    disable();
    teardownInstalled = undefined;
  };

  return createRelease();
}

function createRelease(): () => void {
  let released = false;
  return () => {
    if (released) return;
    released = true;
    installationCount -= 1;
    if (installationCount === 0) {
      teardownInstalled?.();
    }
  };
}
