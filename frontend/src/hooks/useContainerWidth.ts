import { useEffect, useState } from "react";

/**
 * The width of the element `ref` is attached to, kept current by a
 * ResizeObserver; `fallback` until the first measurement (and in tests,
 * where the observer is a stub). A callback ref, so an element that mounts
 * late, after a loading state, is still observed.
 */
export default function useContainerWidth(fallback: number) {
  const [element, setElement] = useState<HTMLElement | null>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    if (!element) return;
    // The observer reports the current size on observe(), so no initial read.
    const observer = new ResizeObserver((entries) => {
      setWidth(entries[0]?.contentRect.width ?? 0);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [element]);
  return { ref: setElement, width: width || fallback };
}
