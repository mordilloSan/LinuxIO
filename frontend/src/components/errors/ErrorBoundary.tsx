// components/ErrorBoundary.tsx
import { Box, Typography } from "@mui/material";
import React, { Component, ReactNode } from "react";

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
          <Box p={2}>
            <Typography color="error">
              Something went wrong in this widget.
            </Typography>
          </Box>
        )
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
