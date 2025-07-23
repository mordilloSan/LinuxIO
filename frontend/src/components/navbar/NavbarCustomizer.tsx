import {
  IconButton,
  Button,
  Tooltip,
  Popover,
  Typography,
  useTheme as useMuiTheme,
} from "@mui/material";
import { Paintbrush } from "lucide-react";
import { useEffect, useState } from "react";
import { ColorPicker, type IColor } from "react-color-palette";

import "react-color-palette/css";
import { DEFAULT_PRIMARY_COLOR } from "@/constants";
import useTheme from "@/hooks/useAppTheme";
import { hexToIColor } from "@/utils/hexToIColor";

function NavbarColorCustomizer() {
  const { primaryColor, setPrimaryColor } = useTheme();
  const muiTheme = useMuiTheme();

  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
  const [color, setColor] = useState<IColor>(() => hexToIColor(primaryColor));

  // Sync picker color when context color changes
  useEffect(() => {
    setColor((prev) => {
      if (prev.hex !== primaryColor) {
        return hexToIColor(primaryColor);
      }
      return prev;
    });
  }, [primaryColor]);

  const handleChangeComplete = (newColor: IColor) => {
    setColor(newColor);
    setPrimaryColor(newColor.hex);
  };

  const handleOpen = (event: React.MouseEvent<HTMLElement>) => {
    setAnchorEl(event.currentTarget);
  };

  const handleClose = () => {
    setAnchorEl(null);
  };

  const open = Boolean(anchorEl);

  return (
    <>
      <Tooltip title="Customize primary color">
        <IconButton color="inherit" onClick={handleOpen} size="large">
          <Paintbrush />
        </IconButton>
      </Tooltip>

      <Popover
        open={open}
        anchorEl={anchorEl}
        onClose={handleClose}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
        slotProps={{
          paper: {
            elevation: 6,
            sx: {
              p: 2,
              bgcolor: muiTheme.palette.background.paper,
              borderRadius: 2,
              width: 250,
            },
          },
        }}
      >
        <Typography variant="h6" gutterBottom>
          Primary Color
        </Typography>

        <ColorPicker
          height={150}
          color={color}
          onChange={setColor}
          onChangeComplete={handleChangeComplete}
          hideInput={["rgb", "hsv"]}
        />

        <Button
          variant="outlined"
          fullWidth
          size="small"
          onClick={() => {
            const reset = hexToIColor(DEFAULT_PRIMARY_COLOR);
            setColor(reset);
            setPrimaryColor(DEFAULT_PRIMARY_COLOR);
          }}
          sx={{ mt: 2 }}
        >
          Reset to Default
        </Button>
      </Popover>
    </>
  );
}

export default NavbarColorCustomizer;
