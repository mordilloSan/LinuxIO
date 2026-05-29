import { Icon } from "@iconify/react";
import React, { useState } from "react";

import GeneralDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

type CompressFormat = "zip" | "tar.gz";

interface FormatOption {
  badge?: string;
  description: string;
  icon: string;
  label: string;
  value: CompressFormat;
}

const FORMAT_OPTIONS: FormatOption[] = [
  {
    value: "zip",
    label: "ZIP",
    icon: "mdi:zip-box",
    description: "Compatible with all systems. Single-threaded compression.",
  },
  {
    value: "tar.gz",
    label: "TAR.GZ",
    icon: "mdi:archive",
    description: "Truly multicore compression via pigz. Faster on large files.",
    badge: "Multicore",
  },
];

interface CompressFormatDialogProps {
  onClose: () => void;
  onConfirm: (format: CompressFormat) => void;
  open: boolean;
}

const CompressFormatDialog: React.FC<CompressFormatDialogProps> = ({
  open,
  onClose,
  onConfirm,
}) => {
  const theme = useAppTheme();
  const [selected, setSelected] = useState<CompressFormat>("zip");

  const handleSubmit: React.SubmitEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    onConfirm(selected);
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <form
        onSubmit={handleSubmit}
        style={{
          padding: theme.spacing(4),
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(3),
        }}
      >
        <AppTypography fontWeight={600} variant="h6">
          Compress
        </AppTypography>

        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(1.5),
          }}
        >
          {FORMAT_OPTIONS.map((opt) => {
            const isSelected = selected === opt.value;
            return (
              <button
                key={opt.value}
                onClick={() => setSelected(opt.value)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: theme.spacing(2),
                  padding: theme.spacing(2),
                  borderRadius: 12,
                  border: `2px solid ${isSelected ? theme.palette.primary.main : theme.palette.divider}`,
                  background: isSelected
                    ? `color-mix(in srgb, ${theme.palette.primary.main} 10%, transparent)`
                    : "transparent",
                  cursor: "pointer",
                  color: "inherit",
                  font: "inherit",
                  textAlign: "left",
                  transition: "border-color 150ms ease, background 150ms ease",
                  width: "100%",
                }}
                type="button"
              >
                <Icon
                  height={28}
                  icon={opt.icon}
                  style={{
                    flexShrink: 0,
                    color: isSelected
                      ? theme.palette.primary.main
                      : theme.palette.text.secondary,
                  }}
                  width={28}
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: theme.spacing(1),
                    }}
                  >
                    <AppTypography fontWeight={600} variant="body1">
                      {opt.label}
                    </AppTypography>
                    {opt.badge && (
                      <span
                        style={{
                          fontSize: "0.65rem",
                          fontWeight: 700,
                          letterSpacing: "0.04em",
                          textTransform: "uppercase",
                          padding: "1px 6px",
                          borderRadius: 4,
                          background: theme.palette.success.main,
                          color: theme.palette.success.contrastText,
                        }}
                      >
                        {opt.badge}
                      </span>
                    )}
                  </div>
                  <AppTypography color="text.secondary" variant="body2">
                    {opt.description}
                  </AppTypography>
                </div>
                {isSelected && (
                  <Icon
                    height={20}
                    icon="mdi:check-circle"
                    style={{ flexShrink: 0, color: theme.palette.primary.main }}
                    width={20}
                  />
                )}
              </button>
            );
          })}
        </div>

        <div
          style={{
            display: "flex",
            gap: theme.spacing(2),
            justifyContent: "flex-end",
            marginTop: theme.spacing(1),
          }}
        >
          <AppButton
            onClick={onClose}
            style={{ color: "var(--mui-palette-text-secondary)" }}
            type="button"
          >
            Cancel
          </AppButton>
          <AppButton autoFocus type="submit">
            Compress
          </AppButton>
        </div>
      </form>
    </GeneralDialog>
  );
};

export default CompressFormatDialog;
