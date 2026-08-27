import { Icon } from "@iconify/react";
import { motion } from "motion/react";
import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";
import {
  DETAIL_PANEL_GAP,
  EASING_STANDARD,
  TRANSITION_DURATION_SLOW_MS,
} from "@/theme/constants";

interface DockerResourceDetailsLayoutProps {
  children: ReactNode;
  onClose: () => void;
  resourceLabel: string;
  subtitle: string;
  summary: ReactNode;
  title: string;
}

const DockerResourceDetailsLayout = ({
  children,
  onClose,
  resourceLabel,
  subtitle,
  summary,
  title,
}: DockerResourceDetailsLayoutProps) => {
  const isCompactLayout = useAppMediaQuery(down("md"));
  const transitionDuration = TRANSITION_DURATION_SLOW_MS / 1000;

  return (
    <motion.div
      layout="position"
      style={{ minHeight: 0 }}
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
            flex: isCompactLayout ? "0 0 auto" : 1,
            minWidth: 0,
            width: isCompactLayout ? "100%" : undefined,
          }}
          transition={{
            delay: 0.04,
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
            delay: 0.08,
            duration: transitionDuration,
            ease: EASING_STANDARD,
          }}
        >
          <FrostedCard
            className="custom-scrollbar"
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
    </motion.div>
  );
};

export default DockerResourceDetailsLayout;
