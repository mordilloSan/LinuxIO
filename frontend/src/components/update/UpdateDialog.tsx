import { Icon } from "@iconify/react";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { useTheme } from "@mui/material/styles";
import { useEffect, useRef } from "react";

import AppButton from "@/components/ui/AppButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
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
    <GeneralDialog
      open={open}
      maxWidth="md"
      fullWidth
      disableEscapeKeyDown={!canClose}
      onClose={canClose ? onClose : undefined}
    >
      <AppDialogTitle>
        {updateComplete
          ? updateSuccess
            ? "Update Complete"
            : "Update Failed"
          : "Updating LinuxIO"}
      </AppDialogTitle>
      <AppDialogContent>
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
              <AppLinearProgress
                variant={progress > 0 ? "determinate" : "indeterminate"}
                value={progress}
                style={{
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
              <AppPaper
                variant="outlined"
                style={{
                  maxHeight: 300,
                  overflowY: "auto",
                  backgroundColor: "var(--mui-palette-grey-900)",
                  color: "var(--mui-palette-grey-100)",
                  padding: 8,
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
              </AppPaper>
            </div>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        {updateComplete && updateSuccess && onContinue && (
          <AppButton onClick={onContinue} variant="contained" color="success">
            Continue to Login
          </AppButton>
        )}
        {updateComplete && !updateSuccess && (
          <AppButton onClick={onClose} variant="contained">
            Close
          </AppButton>
        )}
        {!updateComplete && canClose && (
          <AppButton onClick={onClose} variant="contained">
            Close
          </AppButton>
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
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default UpdateDialog;
