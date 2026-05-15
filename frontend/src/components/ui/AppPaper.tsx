import React from "react";

import "./app-paper.css";

export interface AppPaperProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: "elevation" | "outlined";
}

const AppPaper = React.forwardRef<HTMLDivElement, AppPaperProps>(
  ({ variant = "elevation", className, ...rest }, ref) => {
    const cls = [
      "app-paper",
      variant === "outlined" && "app-paper--outlined",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    return <div ref={ref} className={cls} {...rest} />;
  },
);

AppPaper.displayName = "AppPaper";

export default AppPaper;
