import React from "react";

import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";

interface ErrorStateProps {
  message: string;
  onReset?: () => void;
}

const ErrorState: React.FC<ErrorStateProps> = ({ message, onReset }) => {
  return (
    <AppAlert
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
    </AppAlert>
  );
};

export default ErrorState;
