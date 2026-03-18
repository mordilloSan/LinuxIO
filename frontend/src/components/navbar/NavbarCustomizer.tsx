import { Icon } from "@iconify/react";
import { Popover } from "@mui/material";
import { useTheme as useMuiTheme } from "@mui/material/styles";
import type { CSSProperties } from "react";
import { useMemo, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { iconSize, shadowSm } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";
import { COLOR_TOKENS } from "@/theme/colors";
import { alpha } from "@/utils/color";

function NavbarColorCustomizer() {
  const [primaryColor, setPrimaryColor] = useConfigValue("primaryColor");
  const muiTheme = useMuiTheme();

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
          color="inherit"
          onClick={(e) => setAnchorEl(e.currentTarget)}
          aria-label="Customize primary color"
        >
          <Icon icon="mdi:brush" width={iconSize.md} height={iconSize.md} />
        </AppIconButton>
      </AppTooltip>

      <Popover
        open={open}
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
        slotProps={{
          paper: {
            elevation: 0,
            sx: {
              p: 2,
              bgcolor: muiTheme.palette.background.paper,
              borderRadius: 2,
              boxShadow: shadowSm,
            },
          },
        }}
      >
        <AppTypography variant="h6" gutterBottom>
          Primary Color
        </AppTypography>

        {/* Token swatches */}
        <div
          style={{
            display: "flex",
            gap: muiTheme.spacing(1),
            flexWrap: "wrap",
            marginBottom: muiTheme.spacing(1),
          }}
        >
          {tokenSwatches.map(({ name, hex }) => (
            <button
              key={name}
              onClick={() => setPrimaryColor(name)}
              type="button"
              aria-label={`Set color ${name}`}
              title={`${name} (${hex})`}
              style={{
                width: 28,
                height: 28,
                borderRadius: muiTheme.shape.borderRadius,
                backgroundColor: hex,
                border:
                  muiTheme.palette.mode === "dark"
                    ? `1px solid ${alpha(muiTheme.palette.common.white, 0.3)}`
                    : `1px solid ${alpha(muiTheme.palette.common.black, 0.1)}`,
                cursor: "pointer",
                outline:
                  primaryColor?.toLowerCase() === name.toLowerCase()
                    ? "2px solid currentColor"
                    : "none",
                padding: 0,
              }}
            />
          ))}
        </div>

        <div
          style={{
            display: "flex",
            gap: muiTheme.spacing(1),
            marginTop: muiTheme.spacing(2),
          }}
        >
          <AppButton
            variant="outlined"
            fullWidth
            size="small"
            onClick={() => setPrimaryColor("blue")}
            style={
              {
                marginTop: muiTheme.spacing(2),
                "--_btn-main": COLOR_TOKENS.blue,
                "--_btn-dark": COLOR_TOKENS.blue,
                "--_btn-contrast": muiTheme.palette.common.white,
              } as CSSProperties
            }
          >
            Reset to Default
          </AppButton>
        </div>
      </Popover>
    </>
  );
}

export default NavbarColorCustomizer;
