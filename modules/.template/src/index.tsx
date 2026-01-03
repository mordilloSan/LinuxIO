import React, { useState } from 'react';
import { Box, Typography, Button, useTheme } from '@mui/material';
import RootCard from '@/components/cards/RootCard';
import MetricBar from '@/components/gauge/MetricBar';
import { formatFileSize } from '@/utils/formaters';

// Declare global type for production bundle mode
declare global {
  interface Window {
    LinuxIOModules: Record<string, { default: React.ComponentType }>;
  }
}

function ExampleModule() {
  const [message, setMessage] = useState('');
  const theme = useTheme();

  const handleClick = () => {
    setMessage('Hello from Example Module! 👋');
  };

  return (
    <Box sx={{ p: 4 }}>
      <Typography variant="h3" gutterBottom>
        🧩 Example Module
      </Typography>

      <Typography variant="body1" sx={{ mb: 3, color: 'text.secondary' }}>
        This is a demonstration module showing how to create LinuxIO modules with full access
        to components, theme, and utilities.
      </Typography>

      <Box sx={{ display: 'flex', gap: 3, flexWrap: 'wrap' }}>
        <Box sx={{ flex: '1 1 400px', minWidth: 0 }}>
          <RootCard sx={{ height: '100%' }}>
            <Typography variant="h6" gutterBottom>
              LinuxIO Components
            </Typography>

            <Typography gutterBottom>
              This module has full access to LinuxIO components and theme!
            </Typography>

            <MetricBar
              label="Example Metric"
              percent={75}
              color={theme.palette.primary.main}
            />

            <Button
              variant="contained"
              onClick={handleClick}
              sx={{ mt: 2 }}
              fullWidth
            >
              Click Me
            </Button>

            {message && (
              <Typography sx={{ mt: 2, color: 'success.main' }}>
                ✅ {message}
              </Typography>
            )}
          </RootCard>
        </Box>

        <Box sx={{ flex: '1 1 400px', minWidth: 0 }}>
          <RootCard sx={{ height: '100%' }}>
            <Typography variant="h6" gutterBottom>
              Utilities & Theme
            </Typography>

            <Typography variant="body2" gutterBottom>
              <strong>Formatted file size:</strong> {formatFileSize(1073741824)}
            </Typography>

            <Typography variant="body2" gutterBottom>
              <strong>Theme mode:</strong> {theme.palette.mode}
            </Typography>

            <Typography variant="body2" gutterBottom>
              <strong>Primary color:</strong>{' '}
              <Box
                component="span"
                sx={{
                  display: 'inline-block',
                  width: 16,
                  height: 16,
                  bgcolor: theme.palette.primary.main,
                  borderRadius: 1,
                  verticalAlign: 'middle',
                  ml: 1,
                }}
              />
            </Typography>

            <Typography variant="body2" sx={{ mt: 2, fontFamily: 'monospace', fontSize: '0.75rem' }}>
              💡 Edit this module in:<br />
              modules/example-module/src/index.tsx
            </Typography>
          </RootCard>
        </Box>
      </Box>
    </Box>
  );
}

// REQUIRED: Export default for both HMR (dev) and bundled (prod) loads
export default ExampleModule;

// Export to window.LinuxIOModules for IIFE bundle mode (production)
// This runs only when loaded as a script tag, not when imported as ESM
if (typeof window !== 'undefined') {
  if (!window.LinuxIOModules) {
    window.LinuxIOModules = {};
  }
  if (!window.LinuxIOModules['example-module']) {
    window.LinuxIOModules['example-module'] = { default: ExampleModule };
    console.log('✅ Example Module loaded (bundle mode)');
  }
}
