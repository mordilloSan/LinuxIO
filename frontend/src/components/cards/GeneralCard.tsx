import { Icon } from "@iconify/react";
import {
  Box,
  CardContent,
  Typography,
  FormControl,
  Select,
  MenuItem,
  SelectChangeEvent,
  Tooltip,
} from "@mui/material";
import type { SxProps } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import type { Theme } from "@mui/material/styles";
import React from "react";

import FrostedCard from "./RootCard";

import { cardHeight } from "@/constants";

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

export type GeneralCardProps = SelectProps & {
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
  /** Optional MUI SvgIcon component shown next to `icon_text` in the header. */
  icon?: React.ElementType;
  /** Style overrides forwarded to the `icon` component. */
  iconProps?: { sx?: SxProps<Theme> };
  /** Short string (e.g. temperature) rendered beside `icon`. */
  icon_text?: string;
  /** Shows a colored dot in the header indicating connectivity state. */
  connectionStatus?: ConnectionStatus;
  /** @see {@link ContentLayout} */
  contentLayout?: ContentLayout;
};

const GeneralCard: React.FC<GeneralCardProps> = ({
  title,
  titleColor,
  stats,
  stats2,
  avatarIcon,
  icon: IconComponent,
  iconProps,
  icon_text,
  selectOptions = [],
  selectedOption = "",
  selectedOptionLabel,
  onSelect,
  connectionStatus,
  contentLayout = "equal",
}) => {
  const theme = useTheme();
  const primaryColor = theme.palette.primary.main;

  const [statsFlex, stats2Flex]: [number | string, number | string] = (() => {
    if (contentLayout === "equal") return [1, 1];
    if (contentLayout === "auto") return [1, "0 0 auto"];
    return contentLayout;
  })();

  const handleSelectionChange = (event: SelectChangeEvent) => {
    onSelect?.(event.target.value);
  };

  const statusDot = connectionStatus && (
    <Tooltip
      title={connectionStatus === "online" ? "Connected" : "Disconnected"}
      arrow
    >
      <Box
        sx={{
          width: 10,
          height: 10,
          mb: 0.5,
          borderRadius: "50%",
          bgcolor:
            connectionStatus === "online" ? "success.main" : "error.main",
          flexShrink: 0,
        }}
      />
    </Tooltip>
  );

  const renderSelect = selectOptions.length > 0 && (
    <FormControl
      size="small"
      sx={{
        ml: -2,
        mb: 1,
        minWidth: "auto",
        "& .MuiOutlinedInput-root": {
          color: "text.secondary",
        },
        "& .MuiOutlinedInput-notchedOutline": {
          border: "none",
        },
        "& .MuiSelect-select": {
          padding: "4px 8px",
        },
        "& .MuiSvgIcon-root": {
          color: theme.palette.text.secondary,
          fontSize: 18,
        },
      }}
    >
      <Select
        id="card-select"
        name="cardSelect"
        labelId="card-select-label"
        value={selectedOption}
        onChange={handleSelectionChange}
        displayEmpty
        renderValue={() =>
          selectedOptionLabel ? (
            <Typography variant="body2">{selectedOptionLabel}</Typography>
          ) : (
            <Typography variant="body2" color="text.secondary">
              Select...
            </Typography>
          )
        }
      >
        {selectOptions.map((option, index) => (
          <MenuItem key={option.id ?? index} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </Select>
    </FormControl>
  );

  return (
    <FrostedCard
      elevation={2}
      sx={{
        minHeight: cardHeight,
        m: 1,
        display: "flex",
        flexDirection: "column",
        transition:
          "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out",
        borderBottomWidth: "2px",
        borderBottomStyle: "solid",
        borderBottomColor: `color-mix(in srgb, ${primaryColor}, transparent 70%)`,
        "&:hover": {
          borderBottomWidth: "3px",
          borderBottomColor: primaryColor,
          boxShadow: theme.shadows[10],
          marginBlockEnd: "-1px",
        },
      }}
    >
      <CardContent>
        {/* Header */}
        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            mb: 1,
          }}
        >
          {/* Title and optional extras */}
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            <Typography
              variant="h5"
              sx={{
                fontWeight: "bold",
                transform: "translateY(-1px)",
                ...(titleColor && { color: titleColor }),
              }}
            >
              {title}
            </Typography>

            {statusDot}
            {renderSelect}

            {IconComponent && icon_text && (
              <Box
                sx={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 0,
                  lineHeight: 1,
                  ml: -1,
                  mb: 1,
                }}
              >
                <Box
                  sx={{
                    display: "inline-flex",
                    alignItems: "center",
                    mr: "-4px",
                  }}
                >
                  <IconComponent
                    {...iconProps}
                    sx={{
                      verticalAlign: "middle",
                      color: primaryColor,
                      ...iconProps?.sx,
                    }}
                  />
                </Box>
                <Typography
                  variant="body2"
                  sx={{ color: "grey", ml: 0, lineHeight: 1 }}
                >
                  {icon_text}
                </Typography>
              </Box>
            )}
          </Box>

          {/* Avatar/Icon */}
          <Icon
            icon={avatarIcon}
            width="38px"
            height="38px"
            color={primaryColor}
          />
        </Box>

        {/* Content */}
        {stats2 ? (
          <Box
            sx={{
              mt: 3,
              display: "flex",
              flexDirection: { xs: "row", sm: "row", xl: "row" },
            }}
          >
            <Box
              sx={{
                flex: statsFlex,
                minWidth: 0,
                overflow: "hidden",
                display: "flex",
                alignItems: "center",
                justifyContent: "left",
                "& > *": {
                  minWidth: 0,
                  width: "100%",
                },
                "& .MuiTypography-root": {
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                },
              }}
            >
              {stats}
            </Box>
            <Box
              sx={{
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
            </Box>
          </Box>
        ) : (
          <Box
            sx={{
              mt: 7,
              "& .MuiTypography-root": {
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              },
            }}
          >
            {stats}
          </Box>
        )}
      </CardContent>
    </FrostedCard>
  );
};

export default GeneralCard;
