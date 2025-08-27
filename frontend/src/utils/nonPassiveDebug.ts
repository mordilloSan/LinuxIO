// src/utils/nonPassiveDebug.ts


(() => {
  if (typeof window === "undefined") return;

  const orig = EventTarget.prototype.addEventListener;
  const BLOCKING = new Set(["touchstart", "touchmove", "wheel", "mousewheel", "scroll"]);
  const seen = new WeakMap<EventTarget, Set<string>>();

  function labelTarget(t: EventTarget): string {
    try {
      if (t instanceof Window) return "window";
      if (t instanceof Document) return "document";
      if (t instanceof HTMLElement) {
        const id = t.id ? `#${t.id}` : "";
        const cls = t.className && typeof t.className === "string" ? "." + t.className.split(/\s+/).filter(Boolean).join(".") : "";
        return `<${t.tagName.toLowerCase()}${id}${cls}>`;
      }
    } catch {}
    return String(t);
  }

  EventTarget.prototype.addEventListener = function patched(type: string, listener: any, options?: boolean | AddEventListenerOptions) {
    const needsCheck = BLOCKING.has(type);
    const opts = typeof options === "boolean" ? { capture: options } : (options || {});

    if (needsCheck && !("passive" in opts)) {
      const key = type;
      let set = seen.get(this);
      if (!set) { set = new Set(); seen.set(this, set); }
      if (!set.has(key)) {
        set.add(key);
        // eslint-disable-next-line no-console
        console.warn("[non-passive listener]", type, "on", labelTarget(this), "→ add { passive: true } if you don't call preventDefault()");
        // Optional: uncomment to see where it came from
        // console.trace();
      }
    }

    return orig.call(this, type, listener, options as any);
  };
})();
