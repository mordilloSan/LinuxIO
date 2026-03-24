import React, { useState } from 'react';

import { bindStreamHandlers, decodeString, openExecStream } from '@/api';
import linuxio from '@/api/react-query';
import RootCard from '@/components/cards/RootCard';
import AppButton from '@/components/ui/AppButton';
import { useAppTheme } from '@/theme';
import { formatFileSize } from '@/utils/formaters';

import './component.css';

function ExampleModule() {
  const [message, setMessage] = useState('');
  const [dirListing, setDirListing] = useState('');
  const theme = useAppTheme();

  const {
    data: cpuInfo,
    refetch: refetchCpuInfo
  } = linuxio.system.get_cpu_info.useQuery({ enabled: false });

  const handleClick = () => {
    setMessage('Hello from Example Module! ');
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
    <div className="module-shell">
      <header className="module-header">
        <h1 className="module-title">Example Module</h1>
        <p className="module-copy">
          This example uses app-owned components, semantic HTML, and the
          LinuxIO theme hook instead of MUI or Emotion.
        </p>
      </header>

      <div className="module-grid">
        <RootCard className="module-card">
          <h2 className="module-section-title">LinuxIO Components</h2>
          <p className="module-copy">
            Use shared app primitives where they add value, then keep the rest
            of the module in plain React and CSS.
          </p>

          <div className="module-metric">
            <div className="module-metric__row">
              <span>Example Metric</span>
              <span>75%</span>
            </div>
            <div
              className="module-progress"
              style={{
                '--module-progress-color': theme.palette.primary.main,
                '--module-progress-track': theme.alpha(
                  theme.palette.primary.main,
                  0.18,
                ),
                '--module-progress-value': '75%',
              }}
            >
              <div className="module-progress__bar" />
            </div>
          </div>

          <AppButton variant="contained" onClick={handleClick} fullWidth>
            Click Me
          </AppButton>

          {message ? <p className="module-feedback">{message}</p> : null}
        </RootCard>

        <RootCard className="module-card">
          <h2 className="module-section-title">LinuxIO API Demo</h2>

          <div className="module-actions">
            <AppButton variant="outlined" onClick={getCpuInfo} fullWidth>
              Get CPU Info
            </AppButton>
            <AppButton variant="outlined" onClick={listDirectory} fullWidth>
              List /home
            </AppButton>
            <AppButton variant="outlined" onClick={listDirectory2} fullWidth>
              Show Current User
            </AppButton>
          </div>

          {cpuInfo ? (
            <pre
              className="module-pre"
              style={{
                backgroundColor: theme.palette.background.default,
              }}
            >
              {JSON.stringify(cpuInfo, null, 2)}
            </pre>
          ) : null}

          {dirListing ? (
            <pre
              className="module-pre"
              style={{
                backgroundColor: theme.palette.background.default,
              }}
            >
              {dirListing}
            </pre>
          ) : null}
        </RootCard>

        <RootCard className="module-card">
          <h2 className="module-section-title">Utilities and Theme</h2>

          <dl className="module-meta">
            <div className="module-meta__row">
              <dt>Formatted file size</dt>
              <dd>{formatFileSize(1073741824)}</dd>
            </div>
            <div className="module-meta__row">
              <dt>Theme mode</dt>
              <dd>{theme.palette.mode}</dd>
            </div>
            <div className="module-meta__row">
              <dt>Primary color</dt>
              <dd className="module-color-value">
                {theme.palette.primary.main}
                <span
                  className="module-color-swatch"
                  style={{ backgroundColor: theme.palette.primary.main }}
                />
              </dd>
            </div>
          </dl>

          <p className="module-code">
            Edit this module in:
            <br />
            <code>modules/example-module/src/component.jsx</code>
          </p>
        </RootCard>
      </div>
    </div>
  );
}

export default ExampleModule;
