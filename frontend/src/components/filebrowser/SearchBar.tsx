import { Icon } from "@iconify/react";
import React, { useCallback, useState } from "react";

import "./search-bar.css";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
import { useAppTheme } from "@/theme";

interface SearchBarProps {
  disabled?: boolean;
  onChange: (value: string) => void;
  placeholder?: string;
  value: string;
}

const SearchBar: React.FC<SearchBarProps> = ({
  value,
  onChange,
  placeholder = "Search files...",
  disabled = false,
}) => {
  const theme = useAppTheme();
  const [isFocused, setIsFocused] = useState(false);

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
      className="search-bar"
      disabled={disabled}
      endAdornment={
        value ? (
          <AppIconButton
            aria-label="clear search"
            edge="end"
            onClick={handleClear}
            size="small"
            style={{ padding: 4 }}
          >
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        ) : undefined
      }
      fullWidth
      onBlur={() => setIsFocused(false)}
      onChange={handleChange}
      onFocus={() => setIsFocused(true)}
      placeholder={placeholder}
      size="small"
      startAdornment={
        <Icon
          height={20}
          icon="mdi:magnify"
          style={{
            color: isFocused
              ? theme.palette.primary.main
              : theme.palette.text.secondary,
            transition: "color 0.2s",
          }}
          width={20}
        />
      }
      value={value}
    />
  );
};

export default SearchBar;
