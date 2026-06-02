import { Icon } from "@iconify/react";
import type { CSSProperties } from "react";
import { useMemo, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { iconSize, shadowSm } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";
import { COLOR_TOKENS } from "@/theme/colors";
import { alpha } from "@/utils/color";

function NavbarColorCustomizer() {
  const [primaryColor, setPrimaryColor] = useConfigValue("primaryColor");
  const theme = useAppTheme();

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
          padding: theme.spacing(2),
          background: theme.palette.background.paper,
          boxShadow: shadowSm,
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
            gap: theme.spacing(1),
            flexWrap: "wrap",
            marginBottom: theme.spacing(1),
          }}
        >
          {tokenSwatches.map(({ name, hex }) => (
            <button
              aria-label={`Set color ${name}`}
              key={name}
              onClick={() => setPrimaryColor(name)}
              style={{
                width: 28,
                height: 28,
                borderRadius: theme.shape.borderRadius,
                backgroundColor: hex,
                border:
                  theme.palette.mode === "dark"
                    ? `1px solid ${alpha(theme.palette.common.white, 0.3)}`
                    : `1px solid ${alpha(theme.palette.common.black, 0.1)}`,
                cursor: "pointer",
                outline:
                  primaryColor?.toLowerCase() === name.toLowerCase()
                    ? "2px solid currentColor"
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
            gap: theme.spacing(1),
            marginTop: theme.spacing(2),
          }}
        >
          <AppButton
            fullWidth
            onClick={() => setPrimaryColor("blue")}
            size="small"
            style={
              {
                marginTop: theme.spacing(2),
                "--_btn-main": COLOR_TOKENS.blue,
                "--_btn-dark": COLOR_TOKENS.blue,
                "--_btn-contrast": theme.palette.common.white,
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
