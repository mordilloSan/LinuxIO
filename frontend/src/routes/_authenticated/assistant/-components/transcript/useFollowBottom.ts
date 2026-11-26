import { type RefObject, useEffect, useRef } from "react";

const BOTTOM_SLACK_PX = 24;

const atBottom = (el: HTMLElement) =>
  el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_SLACK_PX;

/**
 * Keeps a scroll container pinned to its newest content until the user scrolls
 * up; following resumes when they scroll back to the bottom or `resetKey`
 * changes (e.g. a new session). It pins after every render of the calling
 * component, so call it from the component that re-renders when the content
 * changes, and whenever the container or one of its children changes size,
 * since content also grows without that render: code is coloured, web fonts
 * swap in, the window shrinks.
 *
 * Only a move up stops following. A scroll event can arrive after the content
 * grew past the hook's own pin, and then reads as "not at the bottom" without
 * the user having moved.
 */
export function useFollowBottom(
  ref: RefObject<HTMLElement | null>,
  resetKey?: unknown,
) {
  const following = useRef(true);
  const lastKey = useRef(resetKey);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let lastTop = el.scrollTop;
    const onScroll = () => {
      if (atBottom(el)) following.current = true;
      else if (el.scrollTop < lastTop) following.current = false;
      lastTop = el.scrollTop;
    };
    const observer = new ResizeObserver(() => {
      if (following.current) el.scrollTop = el.scrollHeight;
    });
    observer.observe(el);
    for (const child of el.children) observer.observe(child);
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      observer.disconnect();
      el.removeEventListener("scroll", onScroll);
    };
  }, [ref]);

  useEffect(() => {
    const el = ref.current;
    if (lastKey.current !== resetKey) {
      lastKey.current = resetKey;
      following.current = true;
    }
    if (el && following.current) el.scrollTop = el.scrollHeight;
  });
}
