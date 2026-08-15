import { Icon } from "@iconify/react";
import {
  memo,
  useId,
  useState,
  type ChangeEvent,
  type CSSProperties,
  type ReactNode,
} from "react";

import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";
import { cardHeight } from "@/theme/constants";
import {
  getAccentCardStyles,
  getFrostedCardLiftShadow,
} from "@/theme/surfaces";

import FrostedCard from "./FrostedCard";

/** A single option rendered inside a card header dropdown. */
export interface SelectOption {
  /** Optional stable key; falls back to array index when omitted. */
  id?: string;
  /** The human-readable label shown in the dropdown. */
  label: string;
  /** The internal value passed to `onSelect`. */
  value: string;
}

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

/** Colored header dot indicating whether the card's data source is reachable. */
export const CardStatusDot = ({ online }: { online: boolean }) => {
  const theme = useAppTheme();

  return (
    <StatusDot
      color={online ? theme.palette.success.main : theme.palette.error.main}
      style={{ marginBottom: 2 }}
      tooltip={online ? "Connected" : "Disconnected"}
    />
  );
};

interface CardHeaderSelectProps {
  onChange: (value: string) => void;
  options: SelectOption[];
  value: string;
}

/** Compact underline-less dropdown for the card header (interface/drive pickers). */
export const CardHeaderSelect = ({
  onChange,
  options,
  value,
}: CardHeaderSelectProps) => {
  const theme = useAppTheme();

  if (options.length === 0) return null;

  const handleSelectionChange = (event: ChangeEvent<HTMLSelectElement>) => {
    onChange(event.target.value);
  };

  return (
    <AppSelect
      disableUnderline
      onChange={handleSelectionChange}
      size="small"
      style={
        {
          marginLeft: 0,
          marginBottom: 2,
          color: theme.palette.text.secondary,
          lineHeight: theme.typography.body2.lineHeight,
          "--app-select-input-font-size": "0.75rem",
        } as CSSProperties
      }
      value={value}
      variant="standard"
    >
      {!value && <option disabled hidden value=""></option>}
      {options.map((option, index) => (
        <option key={option.id ?? index} value={option.value}>
          {option.label}
        </option>
      ))}
    </AppSelect>
  );
};

const badgeStyle: CSSProperties = {
  alignItems: "center",
  background: "none",
  border: 0,
  borderRadius: 4,
  display: "inline-flex",
  gap: 0,
  lineHeight: 1,
  marginBottom: 4,
  marginLeft: -4,
  minWidth: 0,
  padding: 0,
};

interface CardBadgeProps {
  /** Iconify icon ID shown before the text. */
  icon: string;
  /** Short string (e.g. temperature) rendered beside the icon. */
  text: string;
  /** When non-empty, the badge becomes a menu trigger for these options. */
  options?: SelectOption[];
  /** Currently selected option value. */
  selected?: string;
  /** Called when the user picks a different option. */
  onSelect?: (value: string) => void;
}

/** Icon-and-text header badge; opens a picker menu when options are given. */
export const CardBadge = ({
  icon,
  onSelect,
  options,
  selected,
  text,
}: CardBadgeProps) => {
  const theme = useAppTheme();
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
  const menuId = useId();

  const content = (
    <>
      <div
        style={{
          display: "inline-flex",
          alignItems: "center",
          marginRight: -4,
        }}
      >
        <Icon
          color={theme.palette.primary.main}
          height="24px"
          icon={icon}
          width="24px"
        />
      </div>
      <AppTypography
        color="text.secondary"
        style={{ marginLeft: 0, lineHeight: 1 }}
        variant="body2"
      >
        {text}
      </AppTypography>
    </>
  );

  if (!options?.length) {
    return <div style={{ ...badgeStyle, cursor: "default" }}>{content}</div>;
  }

  return (
    <>
      <AppButton
        aria-controls={menuId}
        aria-expanded={Boolean(menuAnchor)}
        aria-haspopup="menu"
        aria-label={`Select ${text}`}
        color="inherit"
        onClick={(event) => setMenuAnchor(event.currentTarget)}
        style={{ ...badgeStyle, cursor: "pointer" }}
      >
        {content}
      </AppButton>
      <AppMenu
        anchorEl={menuAnchor}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        minWidth={180}
        id={menuId}
        onClose={() => setMenuAnchor(null)}
        open={Boolean(menuAnchor)}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        {options.map((opt, i) => (
          <AppMenuItem
            key={opt.id ?? i}
            onClick={() => {
              onSelect?.(opt.value);
              setMenuAnchor(null);
            }}
            selected={opt.value === selected}
          >
            {opt.label}
          </AppMenuItem>
        ))}
      </AppMenu>
    </>
  );
};

export interface DashboardCardProps {
  /** Displayed in the card header. */
  title: string;
  /** Left-column content — typically a vertical list of `Typography` metrics. */
  stats: ReactNode;
  /**
   * Right-column content — typically a chart, gauge, or icon grid.
   * When omitted the card renders `stats` across the full width.
   */
  stats2?: ReactNode;
  /** Iconify icon ID rendered as the card's top-right avatar. */
  avatarIcon: string;
  /**
   * Header widgets rendered after the title — `CardStatusDot`,
   * `CardHeaderSelect`, `CardBadge`.
   */
  headerExtras?: ReactNode;
  /** @see {@link ContentLayout} */
  contentLayout?: ContentLayout;
}

const DashboardCardTitle = memo(function DashboardCardTitle({
  title,
}: {
  title: string;
}) {
  return (
    <AppTypography
      fontWeight={700}
      style={{ transform: "translateY(-1px)" }}
      variant="h5"
    >
      {title}
    </AppTypography>
  );
});

/**
 * Static card shell: it takes no data props and holds no polling state, so it
 * renders once and stays inert while data updates. Anything data-driven —
 * stats columns and header widgets alike — must be passed as an element whose
 * component owns its own query subscription; that confines refetch re-renders
 * to the slot contents instead of the whole card.
 */
const DashboardCard = ({
  title,
  stats,
  stats2,
  avatarIcon,
  headerExtras,
  contentLayout = "equal",
}: DashboardCardProps) => {
  const theme = useAppTheme();
  const primaryColor = theme.palette.primary.main;

  const [statsFlex, stats2Flex]: [number | string, number | string] = (() => {
    if (contentLayout === "equal") return [1, 1];
    if (contentLayout === "auto") return [1, "0 0 auto"];
    return contentLayout;
  })();

  return (
    <FrostedCard
      className="hover-lift dc-accent-card"
      style={
        {
          minHeight: cardHeight,
          display: "flex",
          flexDirection: "column",
          ...getAccentCardStyles(primaryColor),
          "--dc-accent": primaryColor,
          "--dc-hover-shadow": getFrostedCardLiftShadow(theme),
        } as CSSProperties
      }
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
            <DashboardCardTitle title={title} />

            {headerExtras}
          </div>

          {/* Avatar/Icon */}
          <Icon
            color={primaryColor}
            height="38px"
            icon={avatarIcon}
            width="38px"
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
