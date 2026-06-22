import type { Ref, RefCallback } from "react";

/**
 * Combines multiple refs (callback or object) into a single ref callback.
 * Null/undefined refs are ignored.
 */
export function mergeRefs<T>(
  ...refs: Array<Ref<T> | undefined>
): RefCallback<T> {
  return (node) => {
    refs.forEach((ref) => {
      if (!ref) return;
      if (typeof ref === "function") {
        ref(node);
        return;
      }
      ref.current = node;
    });
  };
}
