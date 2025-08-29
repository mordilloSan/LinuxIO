import {
  IconButton,
  Button,
  Tooltip,
  Popover,
  Typography,
  useTheme as useMuiTheme,
  Box,
} from "@mui/material";
import { Paintbrush } from "lucide-react";
import { useMemo, useState } from "react";

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
          size="large"
          aria-label="Customize primary color"
        >
          <Paintbrush size={18} />
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
        <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap", mb: 1 }}>
          {tokenSwatches.map(({ name, hex }) => (
            <Box
              key={name}
              onClick={() => setPrimaryColor(name)}
              role="button"
              aria-label={`Set color ${name}`}
              title={`${name} (${hex})`}
              sx={{
                width: 28,
                height: 28,
                borderRadius: 1,
                bgcolor: hex,
                border:
                  muiTheme.palette.mode === "dark"
                    ? "1px solid rgba(255,255,255,0.3)"
                    : "1px solid rgba(0,0,0,0.1)",
                cursor: "pointer",
                outline:
                  primaryColor?.toLowerCase() === name.toLowerCase()
                    ? "2px solid currentColor"
                    : "none",
              }}
            />
          ))}
        </Box>

        <Box sx={{ display: "flex", gap: 1, mt: 2 }}>
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
        </Box>
      </Popover>
    </>
  );
}

export default NavbarColorCustomizer;
