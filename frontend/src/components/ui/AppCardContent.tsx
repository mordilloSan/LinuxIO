import { forwardRef, type HTMLAttributes } from "react";

import "./app-card-content.css";

export type AppCardContentProps = HTMLAttributes<HTMLDivElement>;

const AppCardContent = forwardRef<HTMLDivElement, AppCardContentProps>(
  ({ className, ...rest }, ref) => {
    const cls = ["app-card-content", className].filter(Boolean).join(" ");
    return <div className={cls} ref={ref} {...rest} />;
  },
);

AppCardContent.displayName = "AppCardContent";

export default AppCardContent;
