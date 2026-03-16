import { Icon } from "@iconify/react";
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  LinearProgress,
  Paper,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import AppTypography from "@/components/ui/AppTypography";
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
}) => {
  const theme = useTheme();
  const outputEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when new output arrives
  useEffect(() => {
    outputEndRef.current?.scrollIntoView({
      behavior: "smooth",
    });
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
        <div
          style={{
            display: "grid",
            gap: theme.spacing(2),
          }}
        >
          {/* Success/Fail banner when complete */}
          {updateComplete && (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(2),
                padding: theme.spacing(2),
                borderRadius: String(theme.shape.borderRadius),
                backgroundColor: updateSuccess
                  ? theme.palette.success.main
                  : theme.palette.error.main,
                color: theme.palette.common.white,
              }}
            >
              {updateSuccess ? (
                <Icon icon="mdi:check-circle" width={28} height={28} />
              ) : (
                <Icon icon="mdi:alert-circle" width={28} height={28} />
              )}
              <div>
                <AppTypography variant="h6" fontWeight={700}>
                  {updateSuccess ? "Update Successful!" : "Update Failed"}
                </AppTypography>
                <AppTypography variant="body2">
                  {updateSuccess ? (
                    targetVersion ? (
                      <>
                        LinuxIO has been updated to {targetVersion}. Click
                        Continue to log in.
                      </>
                    ) : (
                      "LinuxIO has been updated. Click Continue to log in."
                    )
                  ) : (
                    "The update could not be completed. Please check the output below for details."
                  )}
                </AppTypography>
              </div>
            </div>
          )}

          {/* Status - show during update and when failed */}
          {(!updateComplete || (updateComplete && !updateSuccess)) &&
            status && (
              <div>
                <AppTypography
                  variant="body2"
                  color="text.secondary"
                  gutterBottom
                >
                  {updateComplete && !updateSuccess
                    ? "Error Details"
                    : "Status"}
                </AppTypography>
                <AppTypography variant="body1" fontWeight={500}>
                  {status || "Preparing..."}
                </AppTypography>
              </div>
            )}

          {/* Progress bar - hide when complete */}
          {!updateComplete && progress < 100 && (
            <div>
              <AppTypography
                variant="body2"
                color="text.secondary"
                style={{
                  marginBottom: 4,
                }}
              >
                Progress
              </AppTypography>
              <LinearProgress
                variant={progress > 0 ? "determinate" : "indeterminate"}
                value={progress}
                sx={{
                  height: 8,
                  borderRadius: 1,
                }}
              />
            </div>
          )}

          {/* Output console */}
          {output.length > 0 && (
            <div>
              <AppTypography
                variant="body2"
                color="text.secondary"
                gutterBottom
              >
                Installation Output
              </AppTypography>
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
                  <div
                    key={index}
                    style={{
                      whiteSpace: "pre-wrap",
                      marginBottom: theme.spacing(0.5),
                    }}
                  >
                    {line}
                  </div>
                ))}
                <div ref={outputEndRef} />
              </Paper>
            </div>
          )}
        </div>
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
          <AppTypography
            variant="body2"
            color="text.secondary"
            style={{
              paddingLeft: 8,
              paddingRight: 8,
            }}
          >
            Please wait...
          </AppTypography>
        )}
      </DialogActions>
    </Dialog>
  );
};
export default UpdateDialog;
