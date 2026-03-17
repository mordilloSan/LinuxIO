import { Alert } from "@mui/material";
import React from "react";

import AppButton from "@/components/ui/AppButton";

interface ErrorStateProps {
  message: string;
  onReset?: () => void;
}

const ErrorState: React.FC<ErrorStateProps> = ({ message, onReset }) => {
  return (
    <Alert
      severity="error"
      action={
        onReset && (
          <AppButton color="inherit" size="small" onClick={onReset}>
            Go to root
          </AppButton>
        )
      }
    >
      {message}
    </Alert>
  );
};

export default ErrorState;
