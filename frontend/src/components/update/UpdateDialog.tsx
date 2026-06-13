import { Icon } from "@iconify/react";
import { useEffect, useRef } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
interface UpdateDialogProps {
  canClose: boolean;
  onClose?: () => void;
  onContinue?: () => void;
  open: boolean;
  output: string[];
  progress: number;
  status: string;
  targetVersion?: string | null;
  updateComplete?: boolean;
  updateSuccess?: boolean;
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
  const theme = useAppTheme();
  const outputEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when new output arrives
  useEffect(() => {
    outputEndRef.current?.scrollIntoView({
      behavior: "smooth",
    });
  }, [output]);
  return (
    <GeneralDialog
      disableEscapeKeyDown={!canClose}
      fullWidth
      maxWidth="md"
      onClose={canClose ? onClose : undefined}
      open={open}
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
                <Icon height={28} icon="mdi:check-circle" width={28} />
              ) : (
                <Icon height={28} icon="mdi:alert-circle" width={28} />
              )}
              <div>
                <AppTypography fontWeight={700} variant="h6">
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
                  color="text.secondary"
                  gutterBottom
                  variant="body2"
                >
                  {updateComplete && !updateSuccess
                    ? "Error Details"
                    : "Status"}
                </AppTypography>
                <AppTypography fontWeight={500} variant="body1">
                  {status || "Preparing..."}
                </AppTypography>
              </div>
            )}

          {/* Progress bar - hide when complete */}
          {!updateComplete && progress < 100 && (
            <div>
              <AppTypography
                color="text.secondary"
                style={{
                  marginBottom: 4,
                }}
                variant="body2"
              >
                Progress
              </AppTypography>
              <AppLinearProgress
                style={{
                  height: 8,
                  borderRadius: 1,
                }}
                value={progress}
                variant={progress > 0 ? "determinate" : "indeterminate"}
              />
            </div>
          )}

          {/* Output console */}
          {output.length > 0 && (
            <div>
              <AppTypography
                color="text.secondary"
                gutterBottom
                variant="body2"
              >
                Installation Output
              </AppTypography>
              <AppPaper
                style={{
                  maxHeight: 300,
                  overflowY: "auto",
                  backgroundColor: "var(--app-palette-grey-900)",
                  color: "var(--app-palette-grey-100)",
                  padding: 8,
                  fontFamily: "monospace",
                  fontSize: "0.875rem",
                }}
                variant="outlined"
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
          <AppButton color="success" onClick={onContinue} variant="contained">
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
            color="text.secondary"
            style={{
              paddingLeft: 8,
              paddingRight: 8,
            }}
            variant="body2"
          >
            Please wait...
          </AppTypography>
        )}
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default UpdateDialog;
