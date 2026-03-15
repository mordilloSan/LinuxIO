import { Icon } from "@iconify/react";
import {
  IconButton,
  Button,
  Tooltip,
  Popover,
  Typography,
  useTheme as useMuiTheme,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import { useMemo, useState } from "react";

import { iconSize } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";
import { COLOR_TOKENS } from "@/theme/colors";

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
      <Tooltip title="Customize primary color">
        <IconButton
          color="inherit"
          onClick={(e) => setAnchorEl(e.currentTarget)}
          aria-label="Customize primary color"
        >
          <Icon icon="mdi:brush" width={iconSize.md} height={iconSize.md} />
        </IconButton>
      </Tooltip>

      <Popover
        open={open}
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
        slotProps={{
          paper: {
            elevation: 6,
            sx: {
              p: 2,
              bgcolor: muiTheme.palette.background.paper,
              borderRadius: 2,
            },
          },
        }}
      >
        <Typography variant="h6" gutterBottom>
          Primary Color
        </Typography>

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
          <Button
            variant="outlined"
            fullWidth
            size="small"
            onClick={() => setPrimaryColor("blue")}
            sx={{
              mt: 2,
              color: COLOR_TOKENS.blue,
              borderColor: COLOR_TOKENS.blue,
              "&:hover": {
                borderColor: COLOR_TOKENS.blue,
                backgroundColor: COLOR_TOKENS.blue,
              },
            }}
          >
            Reset to Default
          </Button>
        </div>
      </Popover>
    </>
  );
}

export default NavbarColorCustomizer;
