import { createContext } from "react";

import { gridColumnCount } from "@/hooks/useGridColumnCount";
import { CARD_PADDING_SM } from "@/theme/constants";

// The card view's size steps, as the slider beside the view button sets them.
// A step is kept per browser (useFileViewState), so a saved one keeps its
// meaning: these steps and their widths are the ones the slider always had.
export const CARD_SIZE_MIN = 1;
export const CARD_SIZE_MAX = 7;
export const CARD_SIZE_DEFAULT = 3;

export const isCardSize = (value: unknown): value is number =>
  typeof value === "number" &&
  Number.isInteger(value) &&
  value >= CARD_SIZE_MIN &&
  value <= CARD_SIZE_MAX;

/** The gap between cards, across a row and between rows. */
export const CARD_GAP = 12;
/** The card list's own padding, on each side. */
export const CARD_PADDING = 4;
/** FileCard's border, on each side. */
export const CARD_BORDER = 3;

export interface CardGeometry {
  /** The card's height: its icon and its padding and border. */
  height: number;
  iconSize: number;
  /** The narrowest a card may get; the cards share a row's spare width. */
  minWidth: number;
}

/**
 * What one size step draws. The width sets how many cards fit a row; the icon
 * grows with it, and the card's height follows the icon, as in a file
 * manager's zoom. The smallest icon is as tall as the four lines of text a
 * card can have (name, location, size, date), so the text never makes a card
 * taller than its step and the virtual rows can be laid out before they are
 * measured.
 */
export const cardGeometry = (size: number): CardGeometry => {
  const iconSize = 56 + size * 8;
  return {
    height: iconSize + 2 * (CARD_PADDING_SM + CARD_BORDER),
    iconSize,
    minWidth: 140 + size * 40,
  };
};

/**
 * Whether a size step can change anything in a card list this wide: two
 * cards of the smallest size fit a row. Narrower, every step shows one card a
 * row, so the cards keep the default size and the slider is not offered.
 */
export const cardSizeAdjustable = (width: number) =>
  gridColumnCount(width, {
    gap: CARD_GAP,
    minItemWidth: cardGeometry(CARD_SIZE_MIN).minWidth,
    padding: CARD_PADDING,
  }) > 1;

/**
 * The icon size of the cards a card list lays out, which FileCard draws: the
 * list's rows are laid out from the same step, so the two cannot disagree.
 */
export const CardIconSizeContext = createContext(
  cardGeometry(CARD_SIZE_DEFAULT).iconSize,
);
