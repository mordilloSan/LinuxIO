import {
  Box,
  Typography,
  Button,
  LinearProgress,
  Collapse,
} from "@mui/material";
import React from "react";

interface Props {
  onUpdateAll: () => Promise<void>;
  isUpdating: boolean;
  currentPackage: string | null;
  progress: number;
}

const UpdateActions: React.FC<Props> = ({
  onUpdateAll,
  isUpdating,
  currentPackage,
  progress,
}) => {
  return (
    <>
      <Collapse in={isUpdating}>
        <Box sx={{ textAlign: "center", mb: 2 }}>
          <Typography variant="h6" gutterBottom>
            Updating {currentPackage}...
          </Typography>
          <LinearProgress variant="determinate" value={progress} />
          <Typography variant="body2" sx={{ mt: 1 }}>
            {`${Math.round(progress)}% completed`}
          </Typography>
        </Box>
      </Collapse>
      <Box sx={{ display: "flex", justifyContent: "flex-end", pb: 2, px: 2 }}>
        <Button
          variant="contained"
          color="primary"
          onClick={onUpdateAll}
          disabled={isUpdating}
        >
          {isUpdating ? "Updating..." : "Install All Updates"}
        </Button>
      </Box>
    </>
  );
};

export default UpdateActions;
