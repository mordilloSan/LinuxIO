import React, { useState } from 'react';
import { Box, Typography, Button, useTheme } from '@mui/material';
import RootCard from '@/components/cards/RootCard';
import MetricBar from '@/components/gauge/MetricBar';
import { formatFileSize } from '@/utils/formaters';
import linuxio from '@/api/react-query';
import { openExecStream, bindStreamHandlers, decodeString } from '@/api';

function ExampleModule() {
  const [message, setMessage] = useState('');
  const [dirListing, setDirListing] = useState('');
  const theme = useTheme();

  const {
    data: cpuInfo,
    refetch: refetchCpuInfo
  } = linuxio.system.get_cpu_info.useQuery({ enabled: false });

  const handleClick = () => {
    setMessage('Hello from Example Module! 👋');
  };

  const getCpuInfo = async () => {
    setMessage('Loading CPU info...');
    const result = await refetchCpuInfo();
    if (result.isSuccess) {
      setMessage('CPU info loaded successfully!');
    } else if (result.isError) {
      setMessage('Error loading CPU info: ' + result.error.message);
    }
  };

  // Example 2: Use openExecStream for command execution (streaming stdout)
  const listDirectory = () => {
    setMessage('Loading directory listing...');
    setDirListing('');

    let output = '';

    const stream = openExecStream('ls', ['-lh', '/home']);
    if (!stream) {
      setMessage('Failed to open stream');
      return;
    }

    bindStreamHandlers(stream, {
      onData: (data) => {
        const text = decodeString(data);
        output += text;
        setDirListing(output);
      },
      onResult: (result) => {
        setMessage(`Directory listing loaded! Exit code: ${result.data?.exitCode || 0}`);
      },
      onClose: () => {
        if (!output) {
          setMessage('Stream closed');
        }
      }
    });
  };

  // Example 3: Another exec stream example
  const listDirectory2 = () => {
    setMessage('Loading user info...');
    setDirListing('');

    let output = '';

    const stream = openExecStream('whoami', []);
    if (!stream) {
      setMessage('Failed to open stream');
      return;
    }

    bindStreamHandlers(stream, {
      onData: (data) => {
        const text = decodeString(data);
        output += text;
        setDirListing(output);
      },
      onResult: (result) => {
        setMessage(`Done! Exit code: ${result.data?.exitCode || 0}`);
      },
      onClose: () => {
        if (!output) {
          setMessage('Stream closed');
        }
      }
    });
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
              LinuxIO API Demo
            </Typography>

            <Button
              variant="outlined"
              onClick={getCpuInfo}
              sx={{ mt: 1 }}
              fullWidth
            >
              Get CPU Info (system handler)
            </Button>

            <Button
              variant="outlined"
              onClick={listDirectory}
              sx={{ mt: 1 }}
              fullWidth
            >
              List /home (exec stream)
            </Button>

            <Button
              variant="outlined"
              onClick={listDirectory2}
              sx={{ mt: 1 }}
              fullWidth
            >
              who is the user (whoami)
            </Button>

            {cpuInfo && (
              <Box sx={{ mt: 2, p: 1, bgcolor: 'background.default', borderRadius: 1 }}>
                <Typography variant="caption" sx={{ fontFamily: 'monospace', fontSize: '0.7rem' }}>
                  {JSON.stringify(cpuInfo, null, 2)}
                </Typography>
              </Box>
            )}

            {dirListing && (
              <Box sx={{ mt: 2, p: 1, bgcolor: 'background.default', borderRadius: 1 }}>
                <Typography variant="caption" component="pre" sx={{ fontFamily: 'monospace', fontSize: '0.7rem', whiteSpace: 'pre-wrap' }}>
                  {dirListing}
                </Typography>
              </Box>
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
              modules/example-module/src/component.jsx
            </Typography>
          </RootCard>
        </Box>
      </Box>
    </Box>
  );
}

export default ExampleModule;
