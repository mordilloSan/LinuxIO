import React, { useEffect, useImperativeHandle, useRef } from "react";

import "./app-checkbox.css";

export interface AppCheckboxProps extends Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "size" | "type" | "onChange"
> {
  size?: "small" | "medium";
  indeterminate?: boolean;
  color?: "primary" | "error" | "default";
  onChange?: (
    event: React.ChangeEvent<HTMLInputElement>,
    checked: boolean,
  ) => void;
}

const AppCheckbox = React.forwardRef<HTMLInputElement, AppCheckboxProps>(
  (
    {
      size = "medium",
      indeterminate = false,
      color = "primary",
      onChange,
      className,
      ...rest
    },
    ref,
  ) => {
    const inputRef = useRef<HTMLInputElement>(null);
    useImperativeHandle(ref, () => inputRef.current!);

    useEffect(() => {
      if (inputRef.current) {
        inputRef.current.indeterminate = indeterminate;
      }
    }, [indeterminate]);

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
      onChange?.(e, e.target.checked);
    };

    const cls = [
      "app-checkbox",
      size === "small" && "app-checkbox--small",
      color === "error" && "app-checkbox--error",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    return (
      <span className={cls}>
        <input
          ref={inputRef}
          type="checkbox"
          className="app-checkbox__input"
          onChange={handleChange}
          {...rest}
        />
        <svg
          className="app-checkbox__icon"
          focusable="false"
          aria-hidden="true"
          viewBox="0 0 24 24"
        >
          {/* unchecked — outline square */}
          <path
            className="app-checkbox__border"
            d="M19 5v14H5V5h14m0-2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2z"
          />
          {/* checked — filled square + checkmark */}
          <path
            className="app-checkbox__check"
            d="M19 3H5c-1.11 0-2 .89-2 2v14c0 1.1.89 2 2 2h14c1.11 0 2-.9 2-2V5c0-1.11-.89-2-2-2zm-9 14l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"
          />
          {/* indeterminate — filled square + dash */}
          <path
            className="app-checkbox__dash"
            d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-2 10H7v-2h10v2z"
          />
        </svg>
      </span>
    );
  },
);

AppCheckbox.displayName = "AppCheckbox";

export default AppCheckbox;
