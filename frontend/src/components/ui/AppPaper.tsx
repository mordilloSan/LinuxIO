import type { HTMLAttributes, Ref } from "react";

import "./app-paper.css";

export interface AppPaperProps extends HTMLAttributes<HTMLDivElement> {
  ref?: Ref<HTMLDivElement>;
  variant?: "elevation" | "outlined";
}

const AppPaper = ({
  ref,
  variant = "elevation",
  className,
  ...rest
}: AppPaperProps) => {
  const cls = [
    "app-paper",
    variant === "outlined" && "app-paper--outlined",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return <div className={cls} ref={ref} {...rest} />;
};

AppPaper.displayName = "AppPaper";

export default AppPaper;
