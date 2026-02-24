import { Alert, AlertTitle, Box, Collapse, Typography } from "@mui/material";
import React, { useEffect, useState } from "react";

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
        <Alert severity="info" sx={{ mb: 2 }}>
          <AlertTitle>Validating...</AlertTitle>
          Checking compose file syntax and structure.
        </Alert>
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
        <Alert severity="success" sx={{ mb: 2 }} onClose={dismiss}>
          <AlertTitle>Valid</AlertTitle>
          Compose file is valid.
        </Alert>
      </Collapse>
    );
  }

  const errors = validation.errors.filter((e) => e.type === "error");
  const warnings = validation.errors.filter((e) => e.type === "warning");

  return (
    <Box sx={{ mb: 2 }}>
      {errors.length > 0 && (
        <Collapse in={visible}>
          <Alert
            severity="error"
            sx={{ mb: warnings.length > 0 ? 2 : 0 }}
            onClose={dismiss}
          >
            <AlertTitle>Validation Errors ({errors.length})</AlertTitle>
            {errors.map((error, index) => (
              <Box key={index} sx={{ mt: index > 0 ? 1 : 0 }}>
                <Typography variant="body2">
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
                </Typography>
              </Box>
            ))}
          </Alert>
        </Collapse>
      )}

      {warnings.length > 0 && (
        <Collapse in={visible}>
          <Alert severity="warning" onClose={dismiss}>
            <AlertTitle>Warnings ({warnings.length})</AlertTitle>
            {warnings.map((warning, index) => (
              <Box key={index} sx={{ mt: index > 0 ? 1 : 0 }}>
                <Typography variant="body2">
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
                </Typography>
              </Box>
            ))}
          </Alert>
        </Collapse>
      )}
    </Box>
  );
};

export default ComposeValidationFeedback;
