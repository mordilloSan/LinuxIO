import { type HTMLAttributes, type Ref } from "react";

import "./FrostedCard.css";

type FrostedCardProps = HTMLAttributes<HTMLDivElement> & {
  hoverLift?: boolean;
  accent?: boolean;
  ref?: Ref<HTMLDivElement>;
};

const FrostedCard = ({
  ref,
  accent,
  children,
  hoverLift,
  className,
  ...props
}: FrostedCardProps) => {
  const cls = [
    "frosted-card",
    hoverLift && "hover-lift fc-hover-lift",
    accent && "accent-card",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={cls} ref={ref} {...props}>
      {children}
    </div>
  );
};

FrostedCard.displayName = "FrostedCard";

export default FrostedCard;
