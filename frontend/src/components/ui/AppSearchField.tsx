import "./app-search-field.css";
import AppTextField, { type AppTextFieldProps } from "./AppTextField";

const AppSearchField = ({ ref, ...props }: AppTextFieldProps) => {
  const { className, size = "small", ...rest } = props;
  const rootClass = ["app-search-field", className].filter(Boolean).join(" ");

  return <AppTextField className={rootClass} ref={ref} size={size} {...rest} />;
};

AppSearchField.displayName = "AppSearchField";

export default AppSearchField;
