import React, { useRef, useState } from "react";

import "./app-text-field.css";

export interface AppTextFieldProps {
  "aria-autocomplete"?: "none" | "inline" | "list" | "both";
  "aria-controls"?: string;
  "aria-expanded"?: boolean;
  autoComplete?: string;
  autoFocus?: boolean;
  className?: string;
  disabled?: boolean;
  endAdornment?: React.ReactNode;
  error?: boolean;
  fullWidth?: boolean;
  helperText?: React.ReactNode;
  id?: string;
  label?: string;
  list?: string;
  multiline?: boolean;
  name?: string;
  onBlur?: (e: React.FocusEvent) => void;
  onChange?: (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => void;
  onClick?: React.MouseEventHandler<HTMLDivElement>;
  onFocus?: (e: React.FocusEvent) => void;
  onKeyDown?: (e: React.KeyboardEvent) => void;
  placeholder?: string;
  required?: boolean;
  role?: string;
  rows?: number;
  shrinkLabel?: boolean;
  size?: "small" | "medium";
  startAdornment?: React.ReactNode;
  style?: React.CSSProperties;
  type?: string;
  value?: string | number;
  variant?: "outlined" | "standard";
}

const AppTextField = React.forwardRef<
  HTMLInputElement | HTMLTextAreaElement,
  AppTextFieldProps
>((props, ref) => {
  const {
    label,
    value,
    onChange,
    placeholder,
    type,
    size = "medium",
    variant = "outlined",
    fullWidth = false,
    required = false,
    autoFocus = false,
    autoComplete,
    disabled = false,
    error = false,
    helperText,
    multiline = false,
    rows,
    name,
    id,
    startAdornment,
    endAdornment,
    shrinkLabel,
    list,
    className,
    style,
    onFocus,
    onBlur,
    onKeyDown,
    onClick,
    role,
    "aria-controls": ariaControls,
    "aria-expanded": ariaExpanded,
    "aria-autocomplete": ariaAutocomplete,
  } = props;

  const [focused, setFocused] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  const hasValue = value !== undefined && value !== "";
  const labelText = label && required ? `${label}\u2009*` : label;
  const labelShrunk = !!(shrinkLabel || focused || hasValue || startAdornment);

  const handleFocus = (e: React.FocusEvent) => {
    setFocused(true);
    onFocus?.(e);
  };

  const handleBlur = (e: React.FocusEvent) => {
    setFocused(false);
    onBlur?.(e);
  };

  const handleWrapperClick = (event: React.MouseEvent<HTMLDivElement>) => {
    onClick?.(event);
    if (event.defaultPrevented) return;

    const input = wrapperRef.current?.querySelector(
      "input, textarea",
    ) as HTMLElement | null;
    input?.focus();
  };

  const rootClasses = [
    "app-text-field",
    `app-text-field--${variant}`,
    size === "small" && "app-text-field--small",
    fullWidth && "app-text-field--fullwidth",
    focused && "app-text-field--focused",
    error && "app-text-field--error",
    disabled && "app-text-field--disabled",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  const wrapperClasses = [
    "app-text-field__wrapper",
    startAdornment && "app-text-field__wrapper--has-start",
    endAdornment && "app-text-field__wrapper--has-end",
  ]
    .filter(Boolean)
    .join(" ");

  const inputProps = {
    className: "app-text-field__input",
    value,
    onChange,
    placeholder,
    type,
    required,
    autoFocus,
    autoComplete,
    disabled,
    name,
    id,
    onFocus: handleFocus,
    onBlur: handleBlur,
    onKeyDown,
    list,
    role,
    "aria-controls": ariaControls,
    "aria-expanded": ariaExpanded,
    "aria-autocomplete": ariaAutocomplete,
  };

  return (
    <div className={rootClasses} style={style}>
      {label && (
        <label
          className={`app-text-field__label ${labelShrunk ? "app-text-field__label--shrunk" : ""}`}
          data-shrink={labelShrunk ? "true" : "false"}
          htmlFor={id}
        >
          {labelText}
        </label>
      )}
      <div
        className={wrapperClasses}
        onClick={handleWrapperClick}
        ref={wrapperRef}
      >
        {startAdornment && (
          <div className="app-text-field__adornment app-text-field__adornment--start">
            {startAdornment}
          </div>
        )}
        {multiline ? (
          <textarea
            ref={ref as React.Ref<HTMLTextAreaElement>}
            rows={rows}
            {...inputProps}
          />
        ) : (
          <input ref={ref as React.Ref<HTMLInputElement>} {...inputProps} />
        )}
        {endAdornment && (
          <div className="app-text-field__adornment app-text-field__adornment--end">
            {endAdornment}
          </div>
        )}
        {variant === "outlined" && (
          <fieldset
            aria-hidden="true"
            className={`app-text-field__outline ${label ? "app-text-field__outline--with-label" : ""} ${labelShrunk ? "app-text-field__outline--notched" : ""}`}
          >
            <legend className="app-text-field__legend">
              {labelText ? (
                <span>{labelText}</span>
              ) : (
                <span
                  aria-hidden="true"
                  className="app-text-field__legend-zero-width"
                >
                  &#8203;
                </span>
              )}
            </legend>
          </fieldset>
        )}
      </div>
      {helperText && <p className="app-text-field__helper">{helperText}</p>}
    </div>
  );
});

AppTextField.displayName = "AppTextField";

export default AppTextField;
