import { Icon } from "@iconify/react";
import React, { useId, useRef, useState } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTextField from "@/components/ui/AppTextField";
import DirectoryTree from "@/components/ui/DirectoryTree";

interface PathPickerFieldProps {
  browseLabel?: string;
  className?: string;
  disabled?: boolean;
  /** Allow typing a path directly; the tree opens from the folder button instead of the field itself. */
  editable?: boolean;
  error?: boolean;
  fileFilter?: (path: string) => boolean;
  helperText?: React.ReactNode;
  id?: string;
  includeFiles?: boolean;
  label?: string;
  onBlur?: () => void;
  onBrowsePathChange?: (path: string) => void;
  browsePath?: string;
  onPickerClose?: () => void;
  onChange: (path: string) => void;
  placeholder?: string;
  required?: boolean;
  selectableTypes?: Array<"directory" | "file">;
  style?: React.CSSProperties;
  value: string;
}

const PathPickerField: React.FC<PathPickerFieldProps> = ({
  value,
  onChange,
  label = "Directory Path",
  placeholder,
  browseLabel = "Browse folders",
  className,
  editable = false,
  disabled = false,
  error = false,
  fileFilter,
  helperText,
  id,
  includeFiles = false,
  required = false,
  selectableTypes,
  onBlur,
  onBrowsePathChange,
  browsePath,
  onPickerClose,
  style,
}) => {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const handleOpen = () => {
    if (disabled) return;
    setAnchorEl(anchorRef.current);
    setOpen(true);
  };

  const handleClose = () => {
    setOpen(false);
    onPickerClose?.();
  };

  const handleTreeSelect = (path: string) => {
    onChange(path);
    handleClose();
  };

  return (
    <>
      <div className={className} ref={anchorRef} style={style}>
        <AppTextField
          disabled={disabled}
          endAdornment={
            editable ? (
              <AppIconButton
                aria-label={browseLabel}
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
          id={fieldId}
          label={label}
          onBlur={onBlur}
          onChange={
            editable ? (event) => onChange(event.target.value) : undefined
          }
          onClick={editable ? undefined : handleOpen}
          placeholder={
            placeholder ?? (editable ? undefined : "Click to select a folder")
          }
          readOnly={!editable}
          required={required}
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
        onClose={handleClose}
        open={open}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        <DirectoryTree
          fileFilter={fileFilter ? (node) => fileFilter(node.path) : undefined}
          includeFiles={includeFiles}
          onBrowsePathChange={onBrowsePathChange}
          onSelect={handleTreeSelect}
          rootPath={browsePath || "/"}
          selectableTypes={selectableTypes}
          selectedPath={value || browsePath || ""}
        />
      </AppPopover>
    </>
  );
};

export default PathPickerField;
