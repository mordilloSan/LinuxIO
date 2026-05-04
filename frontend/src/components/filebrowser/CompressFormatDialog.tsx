import { Icon } from "@iconify/react";
import React, { useState } from "react";

import GeneralDialog from "../dialog/GeneralDialog";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

type CompressFormat = "zip" | "tar.gz";

interface FormatOption {
  value: CompressFormat;
  label: string;
  icon: string;
  description: string;
  badge?: string;
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
  open: boolean;
  onClose: () => void;
  onConfirm: (format: CompressFormat) => void;
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
    <GeneralDialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <form
        onSubmit={handleSubmit}
        style={{
          padding: theme.spacing(4),
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(3),
        }}
      >
        <AppTypography variant="h6" fontWeight={600}>
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
                type="button"
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
              >
                <Icon
                  icon={opt.icon}
                  width={28}
                  height={28}
                  style={{
                    flexShrink: 0,
                    color: isSelected
                      ? theme.palette.primary.main
                      : theme.palette.text.secondary,
                  }}
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: theme.spacing(1),
                    }}
                  >
                    <AppTypography variant="body1" fontWeight={600}>
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
                  <AppTypography variant="body2" color="text.secondary">
                    {opt.description}
                  </AppTypography>
                </div>
                {isSelected && (
                  <Icon
                    icon="mdi:check-circle"
                    width={20}
                    height={20}
                    style={{ flexShrink: 0, color: theme.palette.primary.main }}
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
            type="button"
            onClick={onClose}
            style={{ color: "var(--mui-palette-text-secondary)" }}
          >
            Cancel
          </AppButton>
          <AppButton type="submit" autoFocus>
            Compress
          </AppButton>
        </div>
      </form>
    </GeneralDialog>
  );
};

export default CompressFormatDialog;
