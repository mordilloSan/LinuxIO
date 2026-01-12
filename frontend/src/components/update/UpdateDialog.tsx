import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  LinearProgress,
  Paper,
  Stack,
  Typography,
} from "@mui/material";
import { useEffect, useRef } from "react";

interface UpdateDialogProps {
  open: boolean;
  status: string;
  progress: number;
  output: string[];
  onClose?: () => void;
  canClose: boolean;
}

const UpdateDialog: React.FC<UpdateDialogProps> = ({
  open,
  status,
  progress,
  output,
  onClose,
  canClose,
}) => {
  const outputEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when new output arrives
  useEffect(() => {
    outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [output]);

  return (
    <Dialog
      open={open}
      maxWidth="md"
      fullWidth
      disableEscapeKeyDown={!canClose}
      onClose={canClose ? onClose : undefined}
    >
      <DialogTitle>Updating LinuxIO</DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          {/* Status */}
          <Box>
            <Typography variant="body2" color="text.secondary" gutterBottom>
              Status
            </Typography>
            <Typography variant="body1" fontWeight="medium">
              {status || "Preparing..."}
            </Typography>
          </Box>

          {/* Progress bar */}
          {progress < 100 && (
            <Box>
              <Typography variant="body2" color="text.secondary" mb={1}>
                Progress
              </Typography>
              <LinearProgress
                variant={progress > 0 ? "determinate" : "indeterminate"}
                value={progress}
                sx={{ height: 8, borderRadius: 1 }}
              />
            </Box>
          )}

          {/* Output console */}
          {output.length > 0 && (
            <Box>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                Installation Output
              </Typography>
              <Paper
                variant="outlined"
                sx={{
                  maxHeight: 300,
                  overflowY: "auto",
                  bgcolor: "grey.900",
                  color: "grey.100",
                  p: 2,
                  fontFamily: "monospace",
                  fontSize: "0.875rem",
                }}
              >
                {output.map((line, index) => (
                  <Box key={index} sx={{ whiteSpace: "pre-wrap", mb: 0.5 }}>
                    {line}
                  </Box>
                ))}
                <div ref={outputEndRef} />
              </Paper>
            </Box>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        {canClose && (
          <Button onClick={onClose} variant="contained">
            Close
          </Button>
        )}
        {!canClose && (
          <Typography variant="body2" color="text.secondary" sx={{ px: 2 }}>
            Please wait...
          </Typography>
        )}
      </DialogActions>
    </Dialog>
  );
};

export default UpdateDialog;
