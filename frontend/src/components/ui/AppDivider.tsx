import React from "react";

import "./app-divider.css";

export interface AppDividerProps extends React.HTMLAttributes<HTMLHRElement> {
  orientation?: "horizontal" | "vertical";
}

const AppDivider = React.forwardRef<HTMLHRElement, AppDividerProps>(
  ({ orientation = "horizontal", className, ...rest }, ref) => {
    const cls = [
      "app-divider",
      orientation === "vertical" && "app-divider--vertical",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    return <hr ref={ref} className={cls} {...rest} />;
  },
);

AppDivider.displayName = "AppDivider";

export default AppDivider;
