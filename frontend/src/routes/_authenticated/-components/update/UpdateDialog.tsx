import { Icon } from "@iconify/react";
import { useEffect, useId, useRef, useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCollapse from "@/components/ui/AppCollapse";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery } from "@/theme";
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
const UpdateDialog = ({
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
}: UpdateDialogProps) => {
  const outputId = useId();
  const outputEndRef = useRef<HTMLDivElement>(null);
  const [outputExpanded, setOutputExpanded] = useState(false);
  const prefersReducedMotion = useAppMediaQuery(
    "(prefers-reduced-motion: reduce)",
  );

  // Smooth scrolling is intentionally visible, so it does not need to block paint.
  useEffect(() => {
    if (!outputExpanded) return;

    outputEndRef.current?.scrollIntoView({
      behavior: prefersReducedMotion ? "auto" : "smooth",
    });
    // `output` intentionally retriggers scrolling as update lines render.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [output, outputExpanded, prefersReducedMotion]);
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
            gap: "var(--app-space-8)",
          }}
        >
          {/* Success/Fail banner when complete */}
          {updateComplete && (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: "var(--app-space-8)",
                padding: "var(--app-space-8)",
                borderRadius: "var(--app-radius-base)",
                backgroundColor: updateSuccess
                  ? "var(--app-palette-success-main)"
                  : "var(--app-palette-error-main)",
                color: "white",
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
              <AppButton
                aria-controls={outputId}
                aria-expanded={outputExpanded}
                color="inherit"
                fullWidth
                onClick={() => setOutputExpanded((expanded) => !expanded)}
                style={{
                  width: "100%",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: "var(--app-space-4)",
                  padding: 0,
                  margin: 0,
                  border: 0,
                  background: "transparent",
                  color: "inherit",
                  cursor: "pointer",
                  textAlign: "left",
                }}
                title={
                  outputExpanded
                    ? "Collapse installation output"
                    : "Expand installation output"
                }
                type="button"
              >
                <AppTypography
                  color="text.secondary"
                  component="span"
                  variant="body2"
                >
                  Installation Output
                </AppTypography>
                <Icon
                  aria-hidden="true"
                  height={18}
                  icon={outputExpanded ? "mdi:chevron-up" : "mdi:chevron-down"}
                  width={18}
                />
              </AppButton>
              <div id={outputId}>
                <AppCollapse in={outputExpanded} unmountOnExit>
                  <AppPaper
                    style={{
                      maxHeight: 300,
                      overflowY: "auto",
                      backgroundColor: "var(--app-palette-grey-900)",
                      padding: 8,
                    }}
                    variant="outlined"
                  >
                    {output.map((line, index) => (
                      <AppTypography
                        color="var(--app-palette-grey-100)"
                        component="div"
                        key={index}
                        style={{
                          whiteSpace: "pre-wrap",
                          marginBottom: "var(--app-space-2)",
                          fontFamily: "var(--app-font-mono)",
                        }}
                        variant="body2"
                      >
                        {line}
                      </AppTypography>
                    ))}
                    <div ref={outputEndRef} />
                  </AppPaper>
                </AppCollapse>
              </div>
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
