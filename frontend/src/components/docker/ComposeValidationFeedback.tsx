import { Collapse } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useEffect, useState } from "react";

import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppTypography from "@/components/ui/AppTypography";

export interface ValidationError {
  line?: number;
  column?: number;
  field?: string;
  message: string;
  type: "error" | "warning";
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
  normalized_content?: string; // Auto-normalized content with container_name added
}

interface ComposeValidationFeedbackProps {
  validation: ValidationResult | null;
  isValidating?: boolean;
}

const ComposeValidationFeedback: React.FC<ComposeValidationFeedbackProps> = ({
  validation,
  isValidating = false,
}) => {
  const theme = useTheme();

  // Track which validation result has been dismissed. When a new validation
  // arrives (different object reference), visible resets automatically without
  // any synchronous setState in the effect body.
  const [dismissedValidation, setDismissedValidation] =
    useState<ValidationResult | null>(null);

  // Only setState inside the timer callback — never synchronously.
  useEffect(() => {
    if (!validation) return;
    const timer = setTimeout(() => setDismissedValidation(validation), 10000);
    return () => clearTimeout(timer);
  }, [validation]);

  if (isValidating) {
    return (
      <Collapse in={true}>
        <AppAlert severity="info" style={{ marginBottom: 16 }}>
          <AppAlertTitle>Validating...</AppAlertTitle>
          Checking compose file syntax and structure.
        </AppAlert>
      </Collapse>
    );
  }

  if (!validation) {
    return null;
  }

  const visible = dismissedValidation !== validation;
  const dismiss = () => setDismissedValidation(validation);

  if (validation.valid && validation.errors.length === 0) {
    return (
      <Collapse in={visible}>
        <AppAlert
          severity="success"
          style={{ marginBottom: 16 }}
          onClose={dismiss}
        >
          <AppAlertTitle>Valid</AppAlertTitle>
          Compose file is valid.
        </AppAlert>
      </Collapse>
    );
  }

  const errors = validation.errors.filter((e) => e.type === "error");
  const warnings = validation.errors.filter((e) => e.type === "warning");

  return (
    <div style={{ marginBottom: theme.spacing(2) }}>
      {errors.length > 0 && (
        <Collapse in={visible}>
          <AppAlert
            severity="error"
            style={{ marginBottom: warnings.length > 0 ? 16 : 0 }}
            onClose={dismiss}
          >
            <AppAlertTitle>Validation Errors ({errors.length})</AppAlertTitle>
            {errors.map((error, index) => (
              <div
                key={index}
                style={{
                  marginTop: index > 0 ? theme.spacing(1) : 0,
                }}
              >
                <AppTypography variant="body2">
                  {error.field && (
                    <strong style={{ marginRight: "8px" }}>
                      {error.field}:
                    </strong>
                  )}
                  {error.message}
                  {error.line && (
                    <span style={{ marginLeft: "8px", opacity: 0.7 }}>
                      (line {error.line})
                    </span>
                  )}
                </AppTypography>
              </div>
            ))}
          </AppAlert>
        </Collapse>
      )}

      {warnings.length > 0 && (
        <Collapse in={visible}>
          <AppAlert severity="warning" onClose={dismiss}>
            <AppAlertTitle>Warnings ({warnings.length})</AppAlertTitle>
            {warnings.map((warning, index) => (
              <div
                key={index}
                style={{
                  marginTop: index > 0 ? theme.spacing(1) : 0,
                }}
              >
                <AppTypography variant="body2">
                  {warning.field && (
                    <strong style={{ marginRight: "8px" }}>
                      {warning.field}:
                    </strong>
                  )}
                  {warning.message}
                  {warning.line && (
                    <span style={{ marginLeft: "8px", opacity: 0.7 }}>
                      (line {warning.line})
                    </span>
                  )}
                </AppTypography>
              </div>
            ))}
          </AppAlert>
        </Collapse>
      )}
    </div>
  );
};

export default ComposeValidationFeedback;
