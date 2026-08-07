import { forwardRef, type ChangeEvent, type InputHTMLAttributes } from "react";

import "./app-switch.css";

export interface AppSwitchProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "size" | "type" | "onChange"
> {
  onChange?: (event: ChangeEvent<HTMLInputElement>, checked: boolean) => void;
  size?: "small" | "medium";
}

const AppSwitch = forwardRef<HTMLInputElement, AppSwitchProps>(
  ({ size = "medium", onChange, className, ...rest }, ref) => {
    const cls = [
      "app-switch",
      size === "small" && "app-switch--small",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    const handleChange = (e: ChangeEvent<HTMLInputElement>) => {
      onChange?.(e, e.target.checked);
    };

    return (
      <label className={cls}>
        <input
          className="app-switch__input"
          onChange={handleChange}
          ref={ref}
          type="checkbox"
          {...rest}
        />
        <span className="app-switch__track" />
        <span className="app-switch__thumb" />
      </label>
    );
  },
);

AppSwitch.displayName = "AppSwitch";

export default AppSwitch;
