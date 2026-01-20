import { Alert, AlertTitle, Box, Collapse, Typography } from "@mui/material";
import React from "react";

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

  if (!validation || (validation.valid && validation.errors.length === 0)) {
    return null;
  }

  const errors = validation.errors.filter((e) => e.type === "error");
  const warnings = validation.errors.filter((e) => e.type === "warning");

  return (
    <Box sx={{ mb: 2 }}>
      {errors.length > 0 && (
        <Collapse in={true}>
          <Alert severity="error" sx={{ mb: warnings.length > 0 ? 2 : 0 }}>
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
        <Collapse in={true}>
          <Alert severity="warning">
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
