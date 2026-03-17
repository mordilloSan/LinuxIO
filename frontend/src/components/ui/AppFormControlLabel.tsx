import React from "react";

import "./app-form-control-label.css";

export interface AppFormControlLabelProps {
  control: React.ReactElement;
  label: React.ReactNode;
  style?: React.CSSProperties;
  className?: string;
}

const AppFormControlLabel: React.FC<AppFormControlLabelProps> = ({
  control,
  label,
  style,
  className,
}) => {
  const cls = ["app-form-control-label", className].filter(Boolean).join(" ");

  return (
    <label className={cls} style={style}>
      {control}
      <span className="app-form-control-label__text">{label}</span>
    </label>
  );
};

export default AppFormControlLabel;
