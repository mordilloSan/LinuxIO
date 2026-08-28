import { Icon } from "@iconify/react";
import { useState, type SubmitEventHandler } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { mixWithTransparency } from "@/theme/surfaces";

import GeneralDialog from "../dialog/GeneralDialog";

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

const CompressFormatDialog = ({
  open,
  onClose,
  onConfirm,
}: CompressFormatDialogProps) => {
  const [selected, setSelected] = useState<CompressFormat>("zip");

  const handleSubmit: SubmitEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    onConfirm(selected);
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <form
        onSubmit={handleSubmit}
        style={{
          padding: "var(--app-space-16)",
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-12)",
        }}
      >
        <AppTypography fontWeight={600} variant="h6">
          Compress
        </AppTypography>

        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "var(--app-space-6)",
          }}
        >
          {FORMAT_OPTIONS.map((opt) => {
            const isSelected = selected === opt.value;
            return (
              <AppButton
                aria-pressed={isSelected}
                color="inherit"
                key={opt.value}
                onClick={() => setSelected(opt.value)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: "var(--app-space-8)",
                  padding: "var(--app-space-8)",
                  borderRadius: "var(--app-radius-lg)",
                  border: `2px solid ${isSelected ? "var(--app-palette-primary-main)" : "var(--app-palette-divider)"}`,
                  background: isSelected
                    ? mixWithTransparency(
                        "var(--app-palette-primary-main)",
                        0.1,
                      )
                    : "transparent",
                  cursor: "pointer",
                  color: "inherit",
                  font: "inherit",
                  textAlign: "left",
                  transition:
                    "border-color 150ms ease, background-color 150ms ease",
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
                      ? "var(--app-palette-primary-main)"
                      : "var(--app-palette-text-secondary)",
                  }}
                  width={28}
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: "var(--app-space-4)",
                    }}
                  >
                    <AppTypography fontWeight={600} variant="body1">
                      {opt.label}
                    </AppTypography>
                    {opt.badge && (
                      <AppTypography
                        component="span"
                        style={{
                          fontWeight: 700,
                          letterSpacing: "0.04em",
                          padding: "1px 6px",
                          borderRadius: 4,
                          background: "var(--app-palette-success-main)",
                          color: "var(--app-palette-success-contrast-text)",
                          textTransform: "uppercase",
                        }}
                        variant="caption"
                      >
                        {opt.badge}
                      </AppTypography>
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
                    style={{
                      flexShrink: 0,
                      color: "var(--app-palette-primary-main)",
                    }}
                    width={20}
                  />
                )}
              </AppButton>
            );
          })}
        </div>

        <div
          style={{
            display: "flex",
            gap: "var(--app-space-8)",
            justifyContent: "flex-end",
            marginTop: "var(--app-space-4)",
          }}
        >
          <AppButton
            onClick={onClose}
            style={{ color: "var(--app-palette-text-secondary)" }}
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
