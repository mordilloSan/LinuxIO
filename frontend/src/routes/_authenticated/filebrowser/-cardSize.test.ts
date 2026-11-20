import { describe, expect, it } from "vitest";

import {
  CARD_SIZE_DEFAULT,
  CARD_SIZE_MAX,
  CARD_SIZE_MIN,
  cardGeometry,
  cardSizeAdjustable,
  isCardSize,
} from "./-cardSize";

describe("cardGeometry", () => {
  it("scales the width, the icon and the height together", () => {
    expect(cardGeometry(CARD_SIZE_MIN)).toEqual({
      height: 86,
      iconSize: 64,
      minWidth: 180,
    });
    expect(cardGeometry(CARD_SIZE_DEFAULT)).toEqual({
      height: 102,
      iconSize: 80,
      minWidth: 260,
    });
    expect(cardGeometry(CARD_SIZE_MAX)).toEqual({
      height: 134,
      iconSize: 112,
      minWidth: 420,
    });
  });

  it("makes every step bigger in all three", () => {
    for (let size = CARD_SIZE_MIN; size < CARD_SIZE_MAX; size += 1) {
      const smaller = cardGeometry(size);
      const bigger = cardGeometry(size + 1);
      expect(bigger.minWidth).toBeGreaterThan(smaller.minWidth);
      expect(bigger.iconSize).toBeGreaterThan(smaller.iconSize);
      expect(bigger.height).toBeGreaterThan(smaller.height);
    }
  });

  it("leaves room beside the icon for four lines of card text", () => {
    // Name, a location (Trash, search results), size and date: 13px and
    // 11px text at a line height of 1.2, and the location's 2px margin.
    const fourLines = 3 * 13 * 1.2 + 11.14 * 1.2 + 2;
    expect(cardGeometry(CARD_SIZE_MIN).iconSize).toBeGreaterThanOrEqual(
      fourLines,
    );
  });
});

describe("cardSizeAdjustable", () => {
  it("is false where two cards of the smallest size do not fit a row", () => {
    // Two 180px cards, the 12px gap between them and the list's 4px
    // padding on each side.
    expect(cardSizeAdjustable(379)).toBe(false);
    expect(cardSizeAdjustable(380)).toBe(true);
    expect(cardSizeAdjustable(0)).toBe(false);
    expect(cardSizeAdjustable(1200)).toBe(true);
  });
});

describe("isCardSize", () => {
  it("takes only a whole step in range", () => {
    expect(isCardSize(CARD_SIZE_MIN)).toBe(true);
    expect(isCardSize(CARD_SIZE_MAX)).toBe(true);
    expect(isCardSize(CARD_SIZE_MAX + 1)).toBe(false);
    expect(isCardSize(0)).toBe(false);
    expect(isCardSize(2.5)).toBe(false);
    expect(isCardSize("3")).toBe(false);
  });
});
