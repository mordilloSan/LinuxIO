import React, { useRef, useState } from "react";

import "./app-text-field.css";

type AppTextFieldSlotProps = {
  input?: {
    startAdornment?: React.ReactNode;
    endAdornment?: React.ReactNode;
  };
  inputLabel?: {
    shrink?: boolean;
  };
};

export interface AppTextFieldProps {
  label?: string;
  value?: string;
  onChange?: (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => void;
  placeholder?: string;
  type?: string;
  size?: "small" | "medium";
  variant?: "outlined" | "standard";
  fullWidth?: boolean;
  required?: boolean;
  autoFocus?: boolean;
  autoComplete?: string;
  disabled?: boolean;
  error?: boolean;
  helperText?: React.ReactNode;
  multiline?: boolean;
  rows?: number;
  name?: string;
  id?: string;
  startAdornment?: React.ReactNode;
  endAdornment?: React.ReactNode;
  shrinkLabel?: boolean;
  className?: string;
  style?: React.CSSProperties;
  onFocus?: (e: React.FocusEvent) => void;
  onBlur?: (e: React.FocusEvent) => void;
  onKeyDown?: (e: React.KeyboardEvent) => void;
  /** @deprecated Use startAdornment/endAdornment and shrinkLabel instead */
  slotProps?: AppTextFieldSlotProps;
}

type AppTextFieldInternalProps = Omit<AppTextFieldProps, "slotProps"> & {
  slotProps?: AppTextFieldSlotProps;
};

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
    className,
    style,
    onFocus,
    onBlur,
    onKeyDown,
    slotProps: legacySlotProps,
  } = props as AppTextFieldInternalProps;

  const [focused, setFocused] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  const resolvedStart =
    startAdornment ?? legacySlotProps?.input?.startAdornment;
  const resolvedEnd = endAdornment ?? legacySlotProps?.input?.endAdornment;
  const forceShrink = shrinkLabel ?? legacySlotProps?.inputLabel?.shrink;

  const hasValue = value !== undefined && value !== "";
  const labelShrunk = forceShrink || focused || hasValue || !!placeholder;

  const handleFocus = (e: React.FocusEvent) => {
    setFocused(true);
    onFocus?.(e);
  };

  const handleBlur = (e: React.FocusEvent) => {
    setFocused(false);
    onBlur?.(e);
  };

  const handleWrapperClick = () => {
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
    resolvedStart && "app-text-field__wrapper--has-start",
    resolvedEnd && "app-text-field__wrapper--has-end",
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
  };

  return (
    <div className={rootClasses} style={style}>
      {label && (
        <label
          className={`app-text-field__label ${labelShrunk ? "app-text-field__label--shrunk" : ""}`}
          htmlFor={id}
        >
          {label}
          {required && " *"}
        </label>
      )}
      <div
        ref={wrapperRef}
        className={wrapperClasses}
        onClick={handleWrapperClick}
      >
        {resolvedStart && (
          <div className="app-text-field__adornment app-text-field__adornment--start">
            {resolvedStart}
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
        {resolvedEnd && (
          <div className="app-text-field__adornment app-text-field__adornment--end">
            {resolvedEnd}
          </div>
        )}
      </div>
      {helperText && <p className="app-text-field__helper">{helperText}</p>}
    </div>
  );
});

AppTextField.displayName = "AppTextField";

export default AppTextField;
