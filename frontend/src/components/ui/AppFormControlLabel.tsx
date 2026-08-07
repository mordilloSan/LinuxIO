import {
  cloneElement,
  type CSSProperties,
  type ReactElement,
  type ReactNode,
} from "react";

import "./app-form-control-label.css";

export interface AppFormControlLabelProps {
  className?: string;
  control: ReactElement;
  disabled?: boolean;
  label: ReactNode;
  style?: CSSProperties;
  /** Passed through to the control child (e.g. Radio value for RadioGroup) */
  value?: string;
}

const AppFormControlLabel = ({
  control,
  label,
  style,
  className,
  disabled,
  value,
}: AppFormControlLabelProps) => {
  const cls = ["app-form-control-label", className].filter(Boolean).join(" ");

  const clonedControl = cloneElement(control, {
    ...(disabled !== undefined && { disabled }),
    ...(value !== undefined && { value }),
  });

  return (
    <label className={cls} style={style}>
      {clonedControl}
      <span className="app-form-control-label__text">{label}</span>
    </label>
  );
};

export default AppFormControlLabel;
