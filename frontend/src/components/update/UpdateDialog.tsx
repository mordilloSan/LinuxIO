import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import ErrorIcon from "@mui/icons-material/Error";
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
  updateComplete?: boolean;
  updateSuccess?: boolean;
  onContinue?: () => void;
  targetVersion?: string | null;
  verifiedVersion?: string | null;
}

const UpdateDialog: React.FC<UpdateDialogProps> = ({
  open,
  status,
  progress,
  output,
  onClose,
  canClose,
  updateComplete = false,
  updateSuccess = false,
  onContinue,
  targetVersion,
  verifiedVersion,
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
      <DialogTitle>
        {updateComplete
          ? updateSuccess
            ? "Update Complete"
            : "Update Failed"
          : "Updating LinuxIO"}
      </DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          {/* Success/Fail banner when complete */}
          {updateComplete && (
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 2,
                p: 2,
                borderRadius: 1,
                bgcolor: updateSuccess ? "success.main" : "error.main",
                color: "white",
              }}
            >
              {updateSuccess ? (
                <CheckCircleIcon fontSize="large" />
              ) : (
                <ErrorIcon fontSize="large" />
              )}
              <Box>
                <Typography variant="h6" fontWeight="bold">
                  {updateSuccess ? "Update Successful!" : "Update Failed"}
                </Typography>
                <Typography variant="body2">
                  {updateSuccess ? (
                    verifiedVersion ? (
                      <>
                        LinuxIO has been updated to {verifiedVersion}. Click
                        Continue to log in.
                      </>
                    ) : (
                      "LinuxIO has been updated. Click Continue to log in."
                    )
                  ) : verifiedVersion &&
                    targetVersion &&
                    verifiedVersion !== targetVersion ? (
                    <>
                      Version mismatch: expected {targetVersion}, got{" "}
                      {verifiedVersion}. Please check the output below.
                    </>
                  ) : (
                    "Please check the output below for details."
                  )}
                </Typography>
              </Box>
            </Box>
          )}

          {/* Status - hide when complete */}
          {!updateComplete && (
            <Box>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                Status
              </Typography>
              <Typography variant="body1" fontWeight="medium">
                {status || "Preparing..."}
              </Typography>
            </Box>
          )}

          {/* Progress bar - hide when complete */}
          {!updateComplete && progress < 100 && (
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
        {updateComplete && updateSuccess && onContinue && (
          <Button
            onClick={onContinue}
            variant="contained"
            color="success"
            size="large"
          >
            Continue to Login
          </Button>
        )}
        {updateComplete && !updateSuccess && (
          <Button onClick={onClose} variant="contained">
            Close
          </Button>
        )}
        {!updateComplete && canClose && (
          <Button onClick={onClose} variant="contained">
            Close
          </Button>
        )}
        {!updateComplete && !canClose && (
          <Typography variant="body2" color="text.secondary" sx={{ px: 2 }}>
            Please wait...
          </Typography>
        )}
      </DialogActions>
    </Dialog>
  );
};

export default UpdateDialog;
