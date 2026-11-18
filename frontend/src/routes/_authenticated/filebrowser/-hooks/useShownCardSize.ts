import { useLayoutEffect, useState, type RefObject } from "react";

import { CARD_SIZE_DEFAULT, cardSizeAdjustable } from "../-cardSize";

const contentWidth = (node: HTMLElement) => {
  const style = getComputedStyle(node);
  return (
    node.clientWidth -
    (parseFloat(style.paddingLeft) || 0) -
    (parseFloat(style.paddingRight) || 0)
  );
};

/**
 * The step a card list shows at, and whether the card-size slider is offered,
 * for the area the list fills (the element `ref` holds, which is there with
 * or without items): the saved step where a step can change the cards there
 * (`cardSizeAdjustable`), the default step elsewhere. The area is measured
 * before the first paint and again on every resize, and only a change of the
 * answer renders again. An area not laid out yet counts as wide enough.
 */
export function useShownCardSize(
  ref: RefObject<HTMLElement | null>,
  savedSize: number,
) {
  const [offered, setOffered] = useState(true);

  useLayoutEffect(() => {
    const node = ref.current;
    if (!node) return;
    const update = (width: number) =>
      setOffered(!(width > 0) || cardSizeAdjustable(width));

    update(contentWidth(node));
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) =>
      update(entry?.contentRect.width ?? 0),
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [ref]);

  return { offered, size: offered ? savedSize : CARD_SIZE_DEFAULT };
}
