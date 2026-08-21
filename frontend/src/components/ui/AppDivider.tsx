import type { HTMLAttributes, Ref } from "react";

import "./app-divider.css";

export interface AppDividerProps extends HTMLAttributes<HTMLHRElement> {
  orientation?: "horizontal" | "vertical";
  ref?: Ref<HTMLHRElement>;
}

const AppDivider = ({
  ref,
  orientation = "horizontal",
  className,
  ...rest
}: AppDividerProps) => {
  const cls = [
    "app-divider",
    orientation === "vertical" && "app-divider--vertical",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return <hr className={cls} ref={ref} {...rest} />;
};

AppDivider.displayName = "AppDivider";

export default AppDivider;
