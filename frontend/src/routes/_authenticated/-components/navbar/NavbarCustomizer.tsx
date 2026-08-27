import { Icon } from "@iconify/react";
import type { CSSProperties } from "react";
import { useMemo, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import { COLOR_TOKENS } from "@/theme/colors";
import { iconSize } from "@/theme/constants";

import "./NavbarCustomizer.css";

function NavbarColorCustomizer() {
  const [primaryColor, setPrimaryColor] = useConfigValue("primaryColor");

  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
  const open = Boolean(anchorEl);

  const tokenSwatches = useMemo(
    () => Object.entries(COLOR_TOKENS).map(([name, hex]) => ({ name, hex })),
    [],
  );

  return (
    <>
      <AppTooltip title="Customize primary color">
        <AppIconButton
          aria-label="Customize primary color"
          color="inherit"
          onClick={(e) => setAnchorEl(e.currentTarget)}
        >
          <Icon height={iconSize.md} icon="mdi:brush" width={iconSize.md} />
        </AppIconButton>
      </AppTooltip>

      <AppPopover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        onClose={() => setAnchorEl(null)}
        open={open}
        paperStyle={{
          padding: "var(--app-space-8)",
        }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <AppTypography gutterBottom variant="h6">
          Primary Color
        </AppTypography>

        {/* Token swatches */}
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-4)",
            flexWrap: "wrap",
            marginBottom: "var(--app-space-4)",
          }}
        >
          {tokenSwatches.map(({ name, hex }) => (
            <AppButton
              aria-label={`Set color ${name}`}
              className="navbar-color-swatch"
              color="inherit"
              key={name}
              onClick={() => setPrimaryColor(name)}
              style={{
                minWidth: 28,
                width: 28,
                height: 28,
                borderRadius: "var(--app-radius-base)",
                backgroundColor: hex,
                cursor: "pointer",
                boxShadow:
                  primaryColor?.toLowerCase() === name.toLowerCase()
                    ? "0 0 0 2px currentColor"
                    : "none",
                padding: 0,
              }}
              title={`${name} (${hex})`}
              type="button"
            />
          ))}
        </div>

        <div
          style={{
            display: "flex",
            gap: "var(--app-space-4)",
            marginTop: "var(--app-space-8)",
          }}
        >
          <AppButton
            fullWidth
            onClick={() => setPrimaryColor("blue")}
            size="small"
            style={
              {
                marginTop: "var(--app-space-8)",
                "--_btn-main": COLOR_TOKENS.blue,
                "--_btn-dark": COLOR_TOKENS.blue,
                "--_btn-contrast": "white",
              } as CSSProperties
            }
            variant="outlined"
          >
            Reset to Default
          </AppButton>
        </div>
      </AppPopover>
    </>
  );
}

export default NavbarColorCustomizer;
