import { forwardRef, HTMLAttributes } from "react";

import "./app-paper.css";

export interface AppPaperProps extends HTMLAttributes<HTMLDivElement> {
  variant?: "elevation" | "outlined";
}

const AppPaper = forwardRef<HTMLDivElement, AppPaperProps>(
  ({ variant = "elevation", className, ...rest }, ref) => {
    const cls = [
      "app-paper",
      variant === "outlined" && "app-paper--outlined",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    return <div className={cls} ref={ref} {...rest} />;
  },
);

AppPaper.displayName = "AppPaper";

export default AppPaper;
