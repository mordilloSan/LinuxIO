import {
  memo,
  type CSSProperties,
  type ElementType,
  type ReactNode,
} from "react";

import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { GAP_SM } from "@/theme/constants";

// Bundles the handful of heading looks this header needs to reproduce:
// "default" is the truncating subtitle1 title used by most card headers,
// "compact" is the smaller subtitle2 title used in dense side panels, and
// "section" is the h3/body2 title used for settings-style sections, which
// also lets its (usually longer) subtitle wrap instead of truncating.
const HEADING_PRESETS = {
  default: {
    component: undefined as ElementType | undefined,
    fontWeight: 700,
    subtitleNoWrap: true,
    titleNoWrap: true,
    titleStyle: { lineHeight: 1.2 } as CSSProperties | undefined,
    variant: "subtitle1" as const,
  },
  compact: {
    component: undefined as ElementType | undefined,
    fontWeight: 700,
    subtitleNoWrap: false,
    titleNoWrap: true,
    titleStyle: undefined as CSSProperties | undefined,
    variant: "subtitle2" as const,
  },
  section: {
    component: "h3" as ElementType,
    fontWeight: 600,
    subtitleNoWrap: false,
    titleNoWrap: false,
    titleStyle: { lineHeight: 1.25 } as CSSProperties | undefined,
    variant: "body2" as const,
  },
};

type HeadingPreset = (typeof HEADING_PRESETS)[keyof typeof HEADING_PRESETS];

export interface CardIconHeaderProps {
  /**
   * Vertical alignment of the icon+title group against `right`. "flex-start"
   * pins both to the top when `right` is taller/shorter than the text block.
   */
  align?: "center" | "flex-start";
  headingVariant?: "compact" | "default" | "section";
  icon: ReactNode;
  /** Tints the icon box with the theme's hover background and primary color, instead of leaving it transparent. */
  iconTint?: boolean;
  /** Content rendered on the right side (chips, buttons, dropdowns…). */
  right?: ReactNode;
  style?: CSSProperties;
  subtitle?: ReactNode;
  title: string;
  /** Content rendered inline, immediately after the title. */
  titleSuffix?: ReactNode;
}

const CardIconHeaderTitle = memo(function CardIconHeaderTitle({
  preset,
  title,
}: {
  preset: HeadingPreset;
  title: string;
}) {
  return (
    <AppTypography
      component={preset.component}
      fontWeight={preset.fontWeight}
      noWrap={preset.titleNoWrap}
      style={preset.titleStyle}
      variant={preset.variant}
    >
      {title}
    </AppTypography>
  );
});

const CardIconHeader = ({
  align = "center",
  headingVariant = "default",
  icon,
  iconTint = false,
  title,
  subtitle,
  titleSuffix,
  right,
  style,
}: CardIconHeaderProps) => {
  const theme = useAppTheme();
  const preset = HEADING_PRESETS[headingVariant];

  return (
    <div
      style={{
        display: "flex",
        alignItems: align,
        justifyContent: "space-between",
        ...style,
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: GAP_SM,
          minWidth: 0,
        }}
      >
        <div
          style={{
            width: 40,
            height: 40,
            borderRadius: 8,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
            ...(iconTint
              ? {
                  background: theme.palette.action.hover,
                  color: theme.palette.primary.main,
                }
              : {}),
          }}
        >
          {icon}
        </div>
        <div style={{ minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: GAP_SM }}>
            <CardIconHeaderTitle preset={preset} title={title} />
            {titleSuffix}
          </div>
          {subtitle !== undefined && (
            <AppTypography
              color="text.secondary"
              noWrap={preset.subtitleNoWrap}
              variant="caption"
            >
              {subtitle}
            </AppTypography>
          )}
        </div>
      </div>
      {right}
    </div>
  );
};

export default CardIconHeader;
