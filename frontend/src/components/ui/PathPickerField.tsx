import { Icon } from "@iconify/react";
import React, { useRef, useState } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTextField from "@/components/ui/AppTextField";
import DirectoryTree from "@/components/ui/DirectoryTree";

interface PathPickerFieldProps {
  disabled?: boolean;
  /** Allow typing a path directly; the tree opens from the folder button instead of the field itself. */
  editable?: boolean;
  error?: boolean;
  helperText?: React.ReactNode;
  label?: string;
  onChange: (path: string) => void;
  placeholder?: string;
  style?: React.CSSProperties;
  value: string;
}

const PathPickerField: React.FC<PathPickerFieldProps> = ({
  value,
  onChange,
  label = "Directory Path",
  placeholder,
  editable = false,
  disabled = false,
  error = false,
  helperText,
  style,
}) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const handleOpen = () => {
    if (disabled) return;
    setAnchorEl(anchorRef.current);
    setOpen(true);
  };

  return (
    <>
      <div ref={anchorRef} style={style}>
        <AppTextField
          disabled={disabled}
          endAdornment={
            editable ? (
              <AppIconButton
                aria-label="Browse folders"
                disabled={disabled}
                onClick={handleOpen}
                size="small"
              >
                <Icon icon="mdi:folder-search-outline" width={18} />
              </AppIconButton>
            ) : (
              <Icon
                icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
                style={{ opacity: 0.5 }}
                width={18}
              />
            )
          }
          error={error}
          fullWidth
          helperText={helperText}
          label={label}
          onChange={
            editable ? (event) => onChange(event.target.value) : undefined
          }
          onClick={editable ? undefined : handleOpen}
          placeholder={
            placeholder ?? (editable ? undefined : "Click to select a folder")
          }
          shrinkLabel={!editable}
          size="small"
          style={editable ? undefined : { cursor: "pointer" }}
          value={value}
        />
      </div>
      <AppPopover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        matchAnchorWidth
        onClose={() => setOpen(false)}
        open={open}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        <DirectoryTree onSelect={onChange} selectedPath={value} />
      </AppPopover>
    </>
  );
};

export default PathPickerField;
