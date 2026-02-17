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
import { useTheme } from "@mui/material/styles";
import React from "react";

import FrostedCard from "./RootCard";

import { cardHeight } from "@/constants";

interface SelectOption {
  value: string;
  label: string;
  id?: string;
}

interface GeneralCardProps {
  title: string;
  stats: React.ReactNode;
  stats2?: React.ReactNode;
  avatarIcon: string;
  icon?: React.ElementType;
  iconProps?: Record<string, any>;
  icon_text?: string;
  selectOptions?: SelectOption[];
  selectedOption?: string;
  selectedOptionLabel?: string;
  onSelect?: (value: string) => void;
  connectionStatus?: "online" | "offline" | "warning" | "error";
}

const GeneralCard: React.FC<GeneralCardProps> = ({
  title,
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
}) => {
  const theme = useTheme();
  const primaryColor = theme.palette.primary.main;

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
              sx={{ fontWeight: "bold", transform: "translateY(-1px)" }}
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
                flex: 1,
                minWidth: 0,
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
                flex: 1,
                minWidth: 0,
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
