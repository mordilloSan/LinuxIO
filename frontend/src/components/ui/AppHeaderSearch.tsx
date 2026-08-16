import { Icon } from "@iconify/react";
import { useCallback, useState, type ChangeEvent, type KeyboardEvent } from "react";

import "./app-header-search.css";

import { useAppTheme } from "@/theme";

import AppIconButton from "./AppIconButton";
import AppTextField from "./AppTextField";

interface AppHeaderSearchProps {
  "aria-label"?: string;
  disabled?: boolean;
  onChange: (value: string) => void;
  placeholder?: string;
  value: string;
}

const AppHeaderSearch = ({
  value,
  onChange,
  placeholder = "Search...",
  disabled = false,
  "aria-label": ariaLabel,
}: AppHeaderSearchProps) => {
  const theme = useAppTheme();
  const [isFocused, setIsFocused] = useState(false);

  const handleClear = useCallback(() => {
    onChange("");
  }, [onChange]);

  const handleChange = useCallback(
    (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      onChange(event.target.value);
    },
    [onChange],
  );

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onChange("");
      }
    },
    [onChange],
  );

  return (
    <AppTextField
      aria-label={ariaLabel ?? placeholder}
      className="app-header-search"
      disabled={disabled}
      endAdornment={
        value ? (
          <AppIconButton
            aria-label="Clear search"
            disabled={disabled}
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
      onKeyDown={handleKeyDown}
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

export default AppHeaderSearch;
