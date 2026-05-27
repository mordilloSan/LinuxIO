import React from "react";

import "./app-form-control-label.css";

export interface AppFormControlLabelProps {
  className?: string;
  control: React.ReactElement;
  disabled?: boolean;
  label: React.ReactNode;
  style?: React.CSSProperties;
  /** Passed through to the control child (e.g. Radio value for RadioGroup) */
  value?: string;
}

const AppFormControlLabel: React.FC<AppFormControlLabelProps> = ({
  control,
  label,
  style,
  className,
  disabled,
  value,
}) => {
  const cls = ["app-form-control-label", className].filter(Boolean).join(" ");

  const clonedControl = React.cloneElement(control, {
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
