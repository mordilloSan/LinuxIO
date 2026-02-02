import {
  Box,
  Button,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import React from "react";

interface SelfTestsTabProps {
  startPending: "short" | "long" | null;
  onRunTest: (testType: "short" | "long") => void;
  selfTestLog?: { standard?: { table?: unknown[] } };
  nvmeSelfTestLog?: { table?: unknown[] };
}

export const SelfTestsTab: React.FC<SelfTestsTabProps> = ({
  startPending,
  onRunTest,
  selfTestLog,
  nvmeSelfTestLog,
}) => {
  return (
    <>
      <Box sx={{ mb: 3 }}>
        <Typography variant="subtitle2" gutterBottom>
          Run SMART Self-Test
        </Typography>
        <Box display="flex" gap={2} alignItems="center">
          <Button
            variant="outlined"
            size="small"
            disabled={startPending !== null}
            onClick={(e) => {
              e.stopPropagation();
              onRunTest("short");
            }}
            startIcon={
              startPending === "short" ? (
                <CircularProgress size={16} />
              ) : undefined
            }
          >
            {startPending === "short" ? "Starting..." : "Short Test"}
          </Button>
          <Button
            variant="outlined"
            size="small"
            disabled={startPending !== null}
            onClick={(e) => {
              e.stopPropagation();
              onRunTest("long");
            }}
            startIcon={
              startPending === "long" ? (
                <CircularProgress size={16} />
              ) : undefined
            }
          >
            {startPending === "long" ? "Starting..." : "Extended Test"}
          </Button>
        </Box>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ mt: 1, display: "block" }}
        >
          Short test takes ~2 minutes. Extended test can take hours depending on
          drive size.
        </Typography>
      </Box>

      <Typography variant="subtitle2" gutterBottom>
        Self-Test History
      </Typography>
      {selfTestLog?.standard?.table &&
      (selfTestLog.standard.table as unknown[]).length > 0 ? (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell sx={{ fontWeight: 600 }}>#</TableCell>
                <TableCell sx={{ fontWeight: 600 }}>Type</TableCell>
                <TableCell sx={{ fontWeight: 600 }}>Status</TableCell>
                <TableCell sx={{ fontWeight: 600 }} align="right">
                  Lifetime Hours
                </TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(
                selfTestLog.standard.table as {
                  num?: number;
                  type?: { string?: string };
                  status?: { string?: string; passed?: boolean };
                  lifetime_hours?: number;
                }[]
              ).map((entry, idx) => (
                <TableRow key={idx}>
                  <TableCell>{entry.num ?? idx + 1}</TableCell>
                  <TableCell>{entry.type?.string || "Unknown"}</TableCell>
                  <TableCell
                    sx={{
                      color: entry.status?.passed
                        ? "success.main"
                        : "error.main",
                    }}
                  >
                    {entry.status?.string || "Unknown"}
                  </TableCell>
                  <TableCell align="right">
                    {entry.lifetime_hours?.toLocaleString() || "N/A"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      ) : nvmeSelfTestLog?.table &&
        (nvmeSelfTestLog.table as unknown[]).length > 0 ? (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell sx={{ fontWeight: 600 }}>Type</TableCell>
                <TableCell sx={{ fontWeight: 600 }}>Result</TableCell>
                <TableCell sx={{ fontWeight: 600 }} align="right">
                  Power On Hours
                </TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(
                nvmeSelfTestLog.table as {
                  self_test_code?: { string?: string };
                  self_test_result?: { string?: string; value?: number };
                  power_on_hours?: number;
                }[]
              ).map((entry, idx) => (
                <TableRow key={idx}>
                  <TableCell>
                    {entry.self_test_code?.string || "Unknown"}
                  </TableCell>
                  <TableCell
                    sx={{
                      color:
                        entry.self_test_result?.value === 0
                          ? "success.main"
                          : "error.main",
                    }}
                  >
                    {entry.self_test_result?.string || "Unknown"}
                  </TableCell>
                  <TableCell align="right">
                    {entry.power_on_hours?.toLocaleString() || "N/A"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      ) : (
        <Typography color="text.secondary">
          No self-test history available.
        </Typography>
      )}
    </>
  );
};
