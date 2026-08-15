import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { motion, type MotionValue } from "motion/react";
import { memo } from "react";

import { useDockMagnification } from "./useDockMagnification";
import type { SidebarItem } from "../sidebar/types";

type DockItemProps = SidebarItem & {
  disabled?: boolean;
  gradient: readonly [string, string];
  mouseX: MotionValue<number>;
};

const DockItem = memo<DockItemProps>(
  ({ to, title, icon, params, disabled = false, gradient, mouseX }) => {
    const { lift, size, tileRef } = useDockMagnification(mouseX);

    const renderIcon = () => {
      if (!icon) return null;
      if (typeof icon === "string")
        return <Icon height={24} icon={icon} width={24} />;
      const IconComponent = icon;
      return <IconComponent />;
    };

    const content = (
      <>
        <span className="app-dock__label">{title}</span>
        <span className="app-dock__dot" />
        <motion.span
          className="app-dock__tile"
          ref={tileRef}
          style={{
            background: `linear-gradient(180deg, ${gradient[0]}, ${gradient[1]})`,
            height: size,
            width: size,
            y: lift,
          }}
        >
          {renderIcon()}
        </motion.span>
      </>
    );

    if (disabled) {
      return (
        <li className="app-dock__item">
          <span
            aria-disabled="true"
            className="app-dock-link app-dock-link--disabled"
          >
            {content}
          </span>
        </li>
      );
    }

    return (
      <li className="app-dock__item">
        <Link
          activeOptions={{ exact: to === "/" }}
          activeProps={{ className: "app-dock-link app-dock-link--active" }}
          className="app-dock-link"
          params={params}
          to={to}
        >
          {content}
        </Link>
      </li>
    );
  },
);

DockItem.displayName = "DockItem";

export default DockItem;
