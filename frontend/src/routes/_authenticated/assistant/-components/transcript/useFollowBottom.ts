import { type RefObject, useEffect, useRef } from "react";

const BOTTOM_SLACK_PX = 24;

/**
 * Keeps a scroll container pinned to its newest content until the user scrolls
 * up (following resumes when `resetKey` changes, e.g. a new session). Runs after every render of the calling component, so call it from the
 * component that re-renders when the content changes.
 */
export function useFollowBottom(
  ref: RefObject<HTMLElement | null>,
  resetKey?: unknown,
) {
  const atBottom = useRef(true);
  const lastKey = useRef(resetKey);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onScroll = () => {
      atBottom.current =
        el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_SLACK_PX;
    };
    el.addEventListener("scroll", onScroll);
    return () => el.removeEventListener("scroll", onScroll);
  }, [ref]);

  useEffect(() => {
    const el = ref.current;
    if (lastKey.current !== resetKey) {
      lastKey.current = resetKey;
      atBottom.current = true;
    }
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  });
}
