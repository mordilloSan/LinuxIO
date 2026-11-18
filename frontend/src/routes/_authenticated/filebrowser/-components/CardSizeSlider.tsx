import AppSlider from "@/components/ui/AppSlider";

import { CARD_SIZE_MAX, CARD_SIZE_MIN } from "../-cardSize";

const SLIDER_WIDTH = 88;

// The slider's own keys. In the mobile Actions menu, Up and Down go on to the
// menu, which moves focus through its stops (AppMenu).
const SIZE_KEYS = new Set([
  "ArrowLeft",
  "ArrowRight",
  "End",
  "Home",
  "PageDown",
  "PageUp",
]);

interface CardSizeSliderProps {
  onChange: (size: number) => void;
  value: number;
}

/**
 * The card view's size, beside the header's view button: each step makes the
 * cards wider, their icon bigger and the cards taller (`cardGeometry`).
 */
const CardSizeSlider = ({ onChange, value }: CardSizeSliderProps) => (
  <AppSlider
    aria-label="Card size"
    max={CARD_SIZE_MAX}
    min={CARD_SIZE_MIN}
    onChange={onChange}
    onKeyDown={(event) => {
      if (SIZE_KEYS.has(event.key)) event.stopPropagation();
    }}
    style={{
      alignSelf: "center",
      flex: "0 0 auto",
      marginInline: "var(--app-space-4)",
      width: SLIDER_WIDTH,
    }}
    title="Card size"
    value={value}
  />
);

export default CardSizeSlider;
