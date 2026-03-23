import { Icon } from "@iconify/react";

import { useAppTheme } from "@/theme";

import React, { useState, useCallback } from "react";

import "./search-bar.css";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
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
  const theme = useAppTheme();
  const [isFocused, setIsFocused] = useState(false);
  const ribbonBackground = alpha(
    theme.palette.text.secondary,
    theme.palette.mode === "dark" ? 0.16 : 0.1,
  );
  const ribbonHoverBackground = alpha(
    theme.palette.text.secondary,
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
    (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      onChange(event.target.value);
    },
    [onChange],
  );

  return (
    <AppTextField
      value={value}
      onChange={handleChange}
      onFocus={() => setIsFocused(true)}
      onBlur={() => setIsFocused(false)}
      placeholder={placeholder}
      disabled={disabled}
      size="small"
      fullWidth
      className="search-bar"
      style={{
        ["--search-bar-bg" as string]: ribbonBackground,
        ["--search-bar-bg-hover" as string]: ribbonHoverBackground,
        ["--search-bar-focus-ring" as string]: isFocused
          ? `0 0 0 1px ${focusRing}`
          : "none",
      }}
      startAdornment={
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
      }
      endAdornment={
        value ? (
          <AppIconButton
            onClick={handleClear}
            size="small"
            edge="end"
            aria-label="clear search"
            style={{ padding: 4 }}
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </AppIconButton>
        ) : undefined
      }
    />
  );
};

export default SearchBar;
