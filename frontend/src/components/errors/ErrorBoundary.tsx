import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { Component, type ErrorInfo, type ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface InternalProps extends Props {
  resetQueryError: () => void;
}

interface State {
  error?: Error;
  hasError: boolean;
}

class ErrorBoundaryInternal extends Component<InternalProps, State> {
  state: State = {
    hasError: false,
  };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("ErrorBoundary caught:", error, info);
  }

  private retry = () => {
    this.props.resetQueryError();
    this.setState({ error: undefined, hasError: false });
  };

  render() {
    if (this.state.hasError) {
      return (
        this.props.fallback ?? (
          <div role="alert" style={{ padding: 8 }}>
            <AppTypography color="error">
              Something went wrong in this widget.
            </AppTypography>
            <AppButton
              onClick={this.retry}
              size="small"
              style={{ marginTop: 8 }}
              variant="outlined"
            >
              Retry widget
            </AppButton>
          </div>
        )
      );
    }

    return this.props.children;
  }
}

const ErrorBoundary = ({ children, fallback }: Props) => {
  const { reset } = useQueryErrorResetBoundary();

  return (
    <ErrorBoundaryInternal fallback={fallback} resetQueryError={reset}>
      {children}
    </ErrorBoundaryInternal>
  );
};

export default ErrorBoundary;
