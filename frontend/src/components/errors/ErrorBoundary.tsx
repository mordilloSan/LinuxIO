// components/ErrorBoundary.tsx
import React, { Component, ReactNode } from "react";

import AppTypography from "@/components/ui/AppTypography";

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
  error?: Error;
}

class ErrorBoundary extends Component<Props, State> {
  state: State = {
    hasError: false,
  };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error("ErrorBoundary caught:", error, info);
  }

  render() {
    if (this.state.hasError) {
      return (
        this.props.fallback || (
          <div style={{ padding: 8 }}>
            <AppTypography color="error">
              Something went wrong in this widget.
            </AppTypography>
          </div>
        )
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
