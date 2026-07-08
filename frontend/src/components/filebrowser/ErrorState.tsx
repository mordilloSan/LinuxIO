import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";

interface ErrorStateProps {
  message: string;
  onReset?: () => void;
}

const ErrorState = ({ message, onReset }: ErrorStateProps) => {
  return (
    <AppAlert
      action={
        onReset && (
          <AppButton color="inherit" onClick={onReset} size="small">
            Go to root
          </AppButton>
        )
      }
      severity="error"
    >
      {message}
    </AppAlert>
  );
};

export default ErrorState;
