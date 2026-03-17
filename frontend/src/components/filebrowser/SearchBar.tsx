import { Icon } from "@iconify/react";
import { InputAdornment, TextField, useTheme } from "@mui/material";

import AppIconButton from "@/components/ui/AppIconButton";
import React, { useState, useCallback } from "react";

import { alpha } from "@/utils/color";

interface SearchBarProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
}

const SearchBar: React.FC<SearchBarProps> = ({
  value,
  onChange,
  placeholder = "Search files...",
  disabled = false,
}) => {
  const theme = useTheme();
  const [isFocused, setIsFocused] = useState(false);
  const ribbonBackground = alpha(
    theme.palette.grey[500],
    theme.palette.mode === "dark" ? 0.16 : 0.1,
  );
  const ribbonHoverBackground = alpha(
    theme.palette.grey[500],
    theme.palette.mode === "dark" ? 0.24 : 0.18,
  );
  const focusRing = alpha(
    theme.palette.primary.main,
    theme.palette.mode === "dark" ? 0.34 : 0.22,
  );

  const handleClear = useCallback(() => {
    onChange("");
  }, [onChange]);

  const handleChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      onChange(event.target.value);
    },
    [onChange],
  );

  return (
    <TextField
      value={value}
      onChange={handleChange}
      onFocus={() => setIsFocused(true)}
      onBlur={() => setIsFocused(false)}
      placeholder={placeholder}
      disabled={disabled}
      size="small"
      fullWidth
      slotProps={{
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <Icon
                icon="mdi:magnify"
                width={20}
                height={20}
                style={{
                  color: isFocused
                    ? theme.palette.primary.main
                    : theme.palette.text.secondary,
                  transition: "color 0.2s",
                }}
              />
            </InputAdornment>
          ),
          endAdornment: value && (
            <InputAdornment position="end">
              <AppIconButton
                onClick={handleClear}
                size="small"
                edge="end"
                aria-label="clear search"
                style={{ padding: 4 }}
              >
                <Icon icon="mdi:close" width={18} height={18} />
              </AppIconButton>
            </InputAdornment>
          ),
        },
      }}
      sx={{
        maxWidth: 400,
        minWidth: 250,
        "& .MuiOutlinedInput-root": {
          borderRadius: "24px",
          backgroundColor: ribbonBackground,
          transition: "all 0.2s",
          boxShadow: isFocused ? `0 0 0 1px ${focusRing}` : "none",
          "& fieldset": {
            border: "none",
          },
          "&.Mui-focused": {
            backgroundColor: ribbonHoverBackground,
          },
          "&:hover": {
            backgroundColor: ribbonHoverBackground,
          },
        },
      }}
    />
  );
};

export default SearchBar;
