import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import {
  motion,
  useSpring,
  useTransform,
  type MotionValue,
} from "motion/react";
import { memo, useRef } from "react";

import type { SidebarItem } from "../sidebar/types";

/* Resting tile size, peak size under the cursor, and how far (px) the
   magnification bulge reaches to either side of the cursor. */
const TILE_SIZE = 40;
const TILE_SIZE_MAX = 64;
const MAGNIFY_RANGE = 140;
/* Positive = downward: tiles hang from the top-mounted panel and dip toward
   the cursor. */
const LIFT_MAX = 8;

const SPRING = { damping: 14, mass: 0.1, stiffness: 170 };

type DockItemProps = SidebarItem & {
  disabled?: boolean;
  gradient: readonly [string, string];
  mouseX: MotionValue<number>;
};

const DockItem = memo<DockItemProps>(
  ({ to, title, icon, params, disabled = false, gradient, mouseX }) => {
    const tileRef = useRef<HTMLSpanElement>(null);

    const distance = useTransform(mouseX, (x) => {
      const bounds = tileRef.current?.getBoundingClientRect();
      if (!bounds) return Infinity;
      return x - bounds.x - bounds.width / 2;
    });

    const size = useSpring(
      useTransform(
        distance,
        [-MAGNIFY_RANGE, 0, MAGNIFY_RANGE],
        [TILE_SIZE, TILE_SIZE_MAX, TILE_SIZE],
      ),
      SPRING,
    );
    const lift = useSpring(
      useTransform(
        distance,
        [-MAGNIFY_RANGE, 0, MAGNIFY_RANGE],
        [0, LIFT_MAX, 0],
      ),
      SPRING,
    );

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
