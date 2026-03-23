import React from "react";

import "./app-search-field.css";

import AppTextField, { type AppTextFieldProps } from "./AppTextField";

const AppSearchField = React.forwardRef<
  HTMLInputElement | HTMLTextAreaElement,
  AppTextFieldProps
>((props, ref) => {
  const { className, size = "small", ...rest } = props;
  const rootClass = ["app-search-field", className].filter(Boolean).join(" ");

  return (
    <AppTextField ref={ref} className={rootClass} size={size} {...rest} />
  );
});

AppSearchField.displayName = "AppSearchField";

export default AppSearchField;