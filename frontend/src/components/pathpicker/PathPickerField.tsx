import { Icon } from "@iconify/react";
import {
  useId,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import DirectoryTree from "@/components/pathpicker/DirectoryTree";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTextField from "@/components/ui/AppTextField";

interface PathPickerFieldProps {
  browseLabel?: string;
  className?: string;
  disabled?: boolean;
  /** Allow typing a path directly; the tree opens from the folder button instead of the field itself. */
  editable?: boolean;
  error?: boolean;
  fileFilter?: (path: string) => boolean;
  helperText?: ReactNode;
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
  style?: CSSProperties;
  value: string;
}

const PathPickerField = ({
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
}: PathPickerFieldProps) => {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  const treeId = `${fieldId}-tree`;
  const anchorRef = useRef<HTMLDivElement>(null);
  const paperRef = useRef<HTMLDivElement>(null);
  // The control that opened the picker; focus returns to it on close.
  const openerRef = useRef<HTMLElement | null>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const openPicker = () => {
    if (disabled) return;
    const anchor = anchorRef.current;
    const focused = document.activeElement;
    openerRef.current =
      focused instanceof HTMLElement && anchor?.contains(focused)
        ? focused
        : (anchor?.querySelector("input") ?? null);
    setAnchorEl(anchor);
    setOpen(true);
  };

  const closePicker = () => {
    // Focus inside the tree would be lost when it unmounts. A click elsewhere
    // still moves focus to what was clicked afterwards.
    if (paperRef.current?.contains(document.activeElement)) {
      openerRef.current?.focus();
    }
    setOpen(false);
    onPickerClose?.();
  };

  const togglePicker = () => {
    if (open) {
      closePicker();
    } else {
      openPicker();
    }
  };

  const handleTreeSelect = (path: string) => {
    onChange(path);
    closePicker();
  };

  // The read-only field behaves like a select: these keys open it.
  const handleFieldKeyDown = (event: KeyboardEvent) => {
    if (event.key !== "Enter" && event.key !== " " && event.key !== "ArrowDown")
      return;
    event.preventDefault();
    if (!open) openPicker();
  };

  // The popover sits at the end of the page, so Tab from the tree would leave
  // the form. It closes the picker and puts focus back on the field instead.
  const handlePopoverKeyDown = (event: KeyboardEvent) => {
    if (event.key !== "Tab") return;
    event.preventDefault();
    closePicker();
  };

  return (
    <>
      <div className={className} ref={anchorRef} style={style}>
        <AppTextField
          aria-controls={!editable && open ? treeId : undefined}
          aria-expanded={editable ? undefined : open}
          aria-haspopup={editable ? undefined : "tree"}
          disabled={disabled}
          endAdornment={
            editable ? (
              <AppIconButton
                aria-controls={open ? treeId : undefined}
                aria-expanded={open}
                aria-haspopup="tree"
                aria-label={browseLabel}
                disabled={disabled}
                onClick={togglePicker}
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
          onClick={editable ? undefined : togglePicker}
          onKeyDown={editable ? undefined : handleFieldKeyDown}
          placeholder={
            placeholder ?? (editable ? undefined : "Click to select a folder")
          }
          readOnly={!editable}
          required={required}
          role={editable ? undefined : "combobox"}
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
        onClose={closePicker}
        open={open}
        paperRef={paperRef}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        <div onKeyDown={handlePopoverKeyDown}>
          <DirectoryTree
            aria-label={browseLabel}
            autoFocus
            fileFilter={
              fileFilter ? (node) => fileFilter(node.path) : undefined
            }
            id={treeId}
            includeFiles={includeFiles}
            onBrowsePathChange={onBrowsePathChange}
            onSelect={handleTreeSelect}
            rootPath={browsePath || "/"}
            selectableTypes={selectableTypes}
            selectedPath={value || browsePath || ""}
          />
        </div>
      </AppPopover>
    </>
  );
};

export default PathPickerField;
