import { Icon } from "@iconify/react";
import React, { useState } from "react";

import FrostedCard from "./RootCard";

import AppCardContent from "@/components/ui/AppCardContent";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppSelect from "@/components/ui/AppSelect";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { cardHeight } from "@/constants";
import { useAppTheme } from "@/theme";
import {
  getAccentCardHoverStyles,
  getAccentCardStyles,
} from "@/theme/surfaces";

/** A single option rendered inside the card's dropdown selector. */
export interface SelectOption {
  /** The internal value passed to `onSelect`. */
  value: string;
  /** The human-readable label shown in the dropdown. */
  label: string;
  /** Optional stable key; falls back to array index when omitted. */
  id?: string;
}

/**
 * Whether the card's data source is reachable.
 * Drives the color of the status indicator dot in the header.
 */
export type ConnectionStatus = "online" | "offline" | "warning" | "error";

/**
 * Controls how horizontal space is divided between the primary stats
 * column (left) and the secondary stats column (right).
 *
 * - `"equal"` — 50 / 50 split (default, both `flex: 1`)
 * - `"auto"` — right column shrinks to its content width; left fills the rest
 * - `[n, m]` — explicit CSS flex ratio, e.g. `[1, 2]` gives the right column twice the space
 *
 * Both columns always have `overflow: hidden` applied so neither can
 * burst out of the card boundary.
 */
export type ContentLayout = "equal" | "auto" | [number, number];

/**
 * Discriminated union that enforces the dropdown props are used together.
 * When `selectOptions` is provided, `onSelect` becomes required.
 * Without `selectOptions`, none of the select props are accepted.
 */
type SelectProps =
  | {
      /** Items to populate the header dropdown. Requires `onSelect`. */
      selectOptions: SelectOption[];
      /** Currently selected value; defaults to `""` when omitted. */
      selectedOption?: string;
      /** Override label shown in the collapsed select trigger. */
      selectedOptionLabel?: string;
      /** Called with the new value whenever the user changes the selection. */
      onSelect: (value: string) => void;
    }
  | {
      selectOptions?: never;
      selectedOption?: never;
      selectedOptionLabel?: never;
      onSelect?: never;
    };

export type DashboardCardProps = SelectProps & {
  /** Displayed in the card header. */
  title: string;
  /** Optional color override for the card title (e.g. "primary.main"). */
  titleColor?: string;
  /** Left-column content — typically a vertical list of `Typography` metrics. */
  stats: React.ReactNode;
  /**
   * Right-column content — typically a chart, gauge, or icon grid.
   * When omitted the card renders `stats` across the full width.
   */
  stats2?: React.ReactNode;
  /** Iconify icon ID rendered as the card's top-right avatar. */
  avatarIcon: string;
  /** Optional Iconify icon ID shown next to `icon_text` in the header. */
  icon?: string;
  /** Short string (e.g. temperature) rendered beside `icon`. */
  icon_text?: string;
  /** Shows a colored dot in the header indicating connectivity state. */
  connectionStatus?: ConnectionStatus;
  /** @see {@link ContentLayout} */
  contentLayout?: ContentLayout;
  /** Options shown when the user clicks the icon-text temperature badge. */
  iconTextSelectOptions?: SelectOption[];
  /** Currently selected icon-text option value. */
  selectedIconTextOption?: string;
  /** Called when the user picks a different icon-text option. */
  onIconTextSelect?: (value: string) => void;
};

const DashboardCard: React.FC<DashboardCardProps> = ({
  title,
  titleColor,
  stats,
  stats2,
  avatarIcon,
  icon,
  icon_text,
  selectOptions = [],
  selectedOption = "",

  onSelect,
  connectionStatus,
  contentLayout = "equal",
  iconTextSelectOptions,
  selectedIconTextOption,
  onIconTextSelect,
}) => {
  const theme = useAppTheme();
  const primaryColor = theme.palette.primary.main;
  const [hovered, setHovered] = useState(false);
  const [iconTextMenuAnchor, setIconTextMenuAnchor] =
    useState<null | HTMLElement>(null);

  const [statsFlex, stats2Flex]: [number | string, number | string] = (() => {
    if (contentLayout === "equal") return [1, 1];
    if (contentLayout === "auto") return [1, "0 0 auto"];
    return contentLayout;
  })();

  const handleSelectionChange = (
    event: React.ChangeEvent<HTMLSelectElement>,
  ) => {
    onSelect?.(event.target.value);
  };

  const statusDot = connectionStatus && (
    <AppTooltip
      title={connectionStatus === "online" ? "Connected" : "Disconnected"}
      arrow
    >
      <div
        style={{
          width: 10,
          height: 10,
          marginBottom: 2,
          borderRadius: "50%",
          backgroundColor:
            connectionStatus === "online"
              ? theme.palette.success.main
              : theme.palette.error.main,
          flexShrink: 0,
        }}
      />
    </AppTooltip>
  );

  const renderSelect = selectOptions.length > 0 && (
    <AppSelect
      size="small"
      variant="standard"
      disableUnderline
      value={selectedOption}
      onChange={handleSelectionChange}
      style={{
        marginLeft: 0,
        marginBottom: 2,
        color: theme.palette.text.secondary,
        fontSize: "0.75rem",
        lineHeight: theme.typography.body2.lineHeight,
      }}
    >
      {!selectedOption && <option value="" disabled hidden></option>}
      {selectOptions.map((option, index) => (
        <option key={option.id ?? index} value={option.value}>
          {option.label}
        </option>
      ))}
    </AppSelect>
  );

  return (
    <FrostedCard
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        minHeight: cardHeight,
        display: "flex",
        flexDirection: "column",
        transition:
          "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out, transform 0.2s",
        ...getAccentCardStyles(primaryColor),
        ...(hovered && getAccentCardHoverStyles(theme, primaryColor)),
      }}
    >
      <AppCardContent>
        {/* Header */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginBottom: 4,
          }}
        >
          {/* Title and optional extras */}
          <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
            <AppTypography
              variant="h5"
              fontWeight={700}
              style={{
                transform: "translateY(-1px)",
                ...(titleColor && { color: titleColor }),
              }}
            >
              {title}
            </AppTypography>

            {statusDot}
            {renderSelect}

            {icon && icon_text && (
              <div
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 0,
                  lineHeight: 1,
                  marginLeft: -4,
                  marginBottom: 4,
                  cursor: iconTextSelectOptions?.length ? "pointer" : "default",
                  borderRadius: 4,
                }}
                onClick={
                  iconTextSelectOptions?.length
                    ? (e) =>
                        setIconTextMenuAnchor(e.currentTarget as HTMLElement)
                    : undefined
                }
              >
                <div
                  style={{
                    display: "inline-flex",
                    alignItems: "center",
                    marginRight: -4,
                  }}
                >
                  <Icon
                    icon={icon}
                    width="24px"
                    height="24px"
                    color={primaryColor}
                  />
                </div>
                <AppTypography
                  variant="body2"
                  color="text.secondary"
                  style={{ marginLeft: 0, lineHeight: 1 }}
                >
                  {icon_text}
                </AppTypography>
              </div>
            )}
            {iconTextSelectOptions && iconTextSelectOptions.length > 0 && (
              <AppMenu
                anchorEl={iconTextMenuAnchor}
                open={Boolean(iconTextMenuAnchor)}
                onClose={() => setIconTextMenuAnchor(null)}
                anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
                transformOrigin={{ vertical: "top", horizontal: "left" }}
                minWidth={180}
              >
                {iconTextSelectOptions.map((opt, i) => (
                  <AppMenuItem
                    key={opt.id ?? i}
                    selected={opt.value === selectedIconTextOption}
                    onClick={() => {
                      onIconTextSelect?.(opt.value);
                      setIconTextMenuAnchor(null);
                    }}
                  >
                    {opt.label}
                  </AppMenuItem>
                ))}
              </AppMenu>
            )}
          </div>

          {/* Avatar/Icon */}
          <Icon
            icon={avatarIcon}
            width="38px"
            height="38px"
            color={primaryColor}
          />
        </div>

        {/* Content */}
        {stats2 ? (
          <div
            style={{
              marginTop: 12,
              display: "flex",
              flexDirection: "row",
              gap: 8,
            }}
          >
            <div
              className="dc-stats-col dc-stats-truncate"
              style={{
                flex: statsFlex,
                minWidth: 0,
                overflow: "hidden",
                display: "flex",
                alignItems: "center",
                justifyContent: "left",
              }}
            >
              {stats}
            </div>
            <div
              style={{
                flex: stats2Flex,
                minWidth: 0,
                overflow: "hidden",
                display: "flex",
                height: 120,
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              {stats2}
            </div>
          </div>
        ) : (
          <div className="dc-stats-truncate" style={{ marginTop: 28 }}>
            {stats}
          </div>
        )}
      </AppCardContent>
    </FrostedCard>
  );
};

export default DashboardCard;
