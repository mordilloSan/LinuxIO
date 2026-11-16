import type { InputHTMLAttributes, Ref } from "react";

import "./app-slider.css";

export interface AppSliderProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "onChange" | "type" | "value"
> {
  max: number;
  min: number;
  /** Every step of a drag or key press. */
  onChange: (value: number) => void;
  /** The user let go: pointer up, key up or focus left. May fire repeatedly. */
  onCommit?: () => void;
  ref?: Ref<HTMLInputElement>;
  step?: number;
  value: number;
}

/**
 * A native range input. A parent that must not act on every step keeps the
 * dragged value itself and acts in onCommit.
 */
const AppSlider = ({
  ref,
  className,
  onBlur,
  onChange,
  onCommit,
  onKeyUp,
  onPointerUp,
  step = 1,
  ...rest
}: AppSliderProps) => (
  <input
    className={["app-slider", className].filter(Boolean).join(" ")}
    onBlur={(event) => {
      onBlur?.(event);
      onCommit?.();
    }}
    onChange={(event) => onChange(Number(event.target.value))}
    onKeyUp={(event) => {
      onKeyUp?.(event);
      onCommit?.();
    }}
    onPointerUp={(event) => {
      onPointerUp?.(event);
      onCommit?.();
    }}
    ref={ref}
    step={step}
    type="range"
    {...rest}
  />
);

AppSlider.displayName = "AppSlider";

export default AppSlider;
