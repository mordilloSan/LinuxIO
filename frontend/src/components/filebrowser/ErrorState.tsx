import React from "react";
import { Alert, Button } from "@mui/material";

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
          <Button color="inherit" size="small" onClick={onReset}>
            Go to root
          </Button>
        )
      }
    >
      {message}
    </Alert>
  );
};

export default ErrorState;
