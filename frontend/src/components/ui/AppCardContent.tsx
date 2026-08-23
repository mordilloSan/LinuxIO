import { type HTMLAttributes, type Ref } from "react";

import "./app-card-content.css";

export type AppCardContentProps = HTMLAttributes<HTMLDivElement> & {
  ref?: Ref<HTMLDivElement>;
};

const AppCardContent = ({ ref, className, ...rest }: AppCardContentProps) => {
  const cls = ["app-card-content", className].filter(Boolean).join(" ");
  return <div className={cls} ref={ref} {...rest} />;
};

AppCardContent.displayName = "AppCardContent";

export default AppCardContent;
