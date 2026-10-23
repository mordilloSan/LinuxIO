import { Icon } from "@iconify/react";
import { motion } from "motion/react";
import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { down } from "@/theme/breakpoints";
import {
  DETAIL_PANEL_GAP,
  EASING_STANDARD,
  TRANSITION_DURATION_STANDARD_MS,
} from "@/theme/constants";

interface ResourceDetailsLayoutProps {
  /** Full-width content under the summary and panel row, such as logs. */
  below?: ReactNode;
  children: ReactNode;
  onClose: () => void;
  resourceLabel: string;
  subtitle: string;
  summary: ReactNode;
  /**
   * "share" gives the summary a third of the row; "content" sizes it to its
   * own width and hands the rest to the panel.
   */
  summaryWidth?: "share" | "content";
  title: string;
}

/**
 * The focused-resource view: its card beside a closable details panel, with
 * optional full-width content below.
 */
const ResourceDetailsLayout = ({
  below,
  children,
  onClose,
  resourceLabel,
  subtitle,
  summary,
  summaryWidth = "share",
  title,
}: ResourceDetailsLayoutProps) => {
  const isCompactLayout = useMediaQuery(down("md"));
  const transitionDuration = TRANSITION_DURATION_STANDARD_MS / 1000;

  return (
    <motion.div
      layout="position"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: DETAIL_PANEL_GAP,
        minHeight: 0,
      }}
      transition={{
        duration: transitionDuration,
        ease: EASING_STANDARD,
      }}
    >
      <div
        style={{
          alignItems: "stretch",
          display: "flex",
          flexDirection: isCompactLayout ? "column" : "row",
          gap: DETAIL_PANEL_GAP,
          minHeight: 0,
        }}
      >
        <motion.div
          animate={{ opacity: 1, y: 0 }}
          initial={{ opacity: 0, y: 12 }}
          style={{
            display: "flex",
            flex: isCompactLayout
              ? "0 0 auto"
              : summaryWidth === "content"
                ? "0 0 max-content"
                : 1,
            minWidth: 0,
            width: isCompactLayout ? "100%" : undefined,
          }}
          transition={{
            delay: 0,
            duration: transitionDuration,
            ease: EASING_STANDARD,
          }}
        >
          {summary}
        </motion.div>

        <motion.div
          animate={{ opacity: 1, x: 0, y: 0 }}
          initial={{
            opacity: 0,
            x: isCompactLayout ? 0 : 40,
            y: isCompactLayout ? 20 : 0,
          }}
          style={{
            display: "flex",
            flex: isCompactLayout ? "0 0 auto" : 2,
            minWidth: 0,
            width: isCompactLayout ? "100%" : undefined,
          }}
          transition={{
            delay: 0.1,
            duration: transitionDuration,
            ease: EASING_STANDARD,
          }}
        >
          <FrostedCard
            style={{
              display: "flex",
              flex: 1,
              flexDirection: "column",
              gap: "var(--app-space-4)",
              minHeight: 0,
              minWidth: 0,
              overflowY: "auto",
              padding: 12,
            }}
          >
            <div
              style={{
                alignItems: "flex-start",
                display: "flex",
                gap: "var(--app-space-4)",
                justifyContent: "space-between",
                minWidth: 0,
              }}
            >
              <div style={{ flex: 1, minWidth: 0 }}>
                <AppTypography
                  component="div"
                  fontWeight={700}
                  noWrap
                  title={title}
                  variant="body2"
                >
                  {title}
                </AppTypography>
                <AppTypography
                  color="text.secondary"
                  component="div"
                  noWrap
                  variant="caption"
                >
                  {subtitle}
                </AppTypography>
              </div>
              <AppIconButton
                aria-label={`Close ${resourceLabel} details`}
                onClick={onClose}
                size="small"
              >
                <Icon height={18} icon="mdi:close" width={18} />
              </AppIconButton>
            </div>

            {children}
          </FrostedCard>
        </motion.div>
      </div>
      {below && (
        <motion.div
          animate={{ opacity: 1, y: 0 }}
          initial={{ opacity: 0, y: 20 }}
          style={{ minWidth: 0 }}
          transition={{
            delay: 0.2,
            duration: transitionDuration,
            ease: EASING_STANDARD,
          }}
        >
          {below}
        </motion.div>
      )}
    </motion.div>
  );
};

export default ResourceDetailsLayout;
