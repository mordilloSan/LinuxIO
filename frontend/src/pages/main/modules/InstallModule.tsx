import {
  Box,
  TextField,
  Button,
  FormControlLabel,
  Checkbox,
  Alert,
  Paper,
  Typography,
  CircularProgress,
} from "@mui/material";
import React, { useState } from "react";
import { toast } from "sonner";

import linuxio from "@/api/react-query";
import type { ValidationResult, InstallResult } from "@/types/module";

interface InstallModuleProps {
  onInstalled: () => void;
}

const InstallModule: React.FC<InstallModuleProps> = ({ onInstalled }) => {
  const [path, setPath] = useState("");
  const [targetName, setTargetName] = useState("");
  const [createSymlink, setCreateSymlink] = useState(false);
  const [validationResult, setValidationResult] =
    useState<ValidationResult | null>(null);

  // Mutations
  const { mutate: validateMutation, isPending: validatePending } =
    linuxio.modules.ValidateModule.useMutation();

  const { mutate: installMutation, isPending: installPending } =
    linuxio.modules.InstallModule.useMutation();

  const handleValidate = () => {
    if (!path) {
      toast.error("Please enter a module path");
      return;
    }

    setValidationResult(null);
    validateMutation([path], {
      onSuccess: (result) => {
        setValidationResult(result);
        if (result.valid) {
          toast.success("Module is valid!");
        } else {
          toast.error("Module validation failed");
        }
      },
      onError: (err) => {
        setValidationResult({ valid: false, errors: [err.message] });
      },
    });
  };

  const handleInstall = () => {
    if (!path) {
      toast.error("Please enter a module path");
      return;
    }

    installMutation([path, targetName, createSymlink ? "true" : "false"], {
      onSuccess: (result: InstallResult) => {
        toast.success(result.message || "Module installed successfully!");

        // Reset form
        setPath("");
        setTargetName("");
        setCreateSymlink(false);
        setValidationResult(null);

        // Navigate to installed modules
        onInstalled();
      },
    });
  };

  return (
    <Box sx={{ mt: 2, maxWidth: 800 }}>
      <Paper sx={{ p: 3 }}>
        <Typography variant="h6" gutterBottom>
          Install Module from Path
        </Typography>
        <Typography
          variant="body2"
          color="text.secondary"
          component="p"
          sx={{ mb: 2 }}
        >
          Install a module from a local filesystem path. The module must contain
          a valid <code>module.yaml</code> file.
        </Typography>

        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <TextField
            label="Module Path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/path/to/module"
            fullWidth
            helperText="Absolute path to the module directory"
          />

          <TextField
            label="Target Name (Optional)"
            value={targetName}
            onChange={(e) => setTargetName(e.target.value)}
            placeholder="Leave empty to use module's name"
            fullWidth
            helperText="Custom name for the installed module"
          />

          <FormControlLabel
            control={
              <Checkbox
                checked={createSymlink}
                onChange={(e) => setCreateSymlink(e.target.checked)}
              />
            }
            label="Create symlink (for development)"
          />

          <Box sx={{ display: "flex", gap: 2 }}>
            <Button
              variant="outlined"
              onClick={handleValidate}
              disabled={validatePending || installPending || !path}
            >
              {validatePending ? <CircularProgress size={20} /> : "Validate"}
            </Button>
            <Button
              variant="contained"
              onClick={handleInstall}
              disabled={validatePending || installPending || !path}
            >
              {installPending ? <CircularProgress size={20} /> : "Install"}
            </Button>
          </Box>

          {/* Validation Results */}
          {validationResult && (
            <Box sx={{ mt: 2 }}>
              {validationResult.valid ? (
                <Alert severity="success">
                  Module is valid!
                  {validationResult.manifest && (
                    <Box sx={{ mt: 1 }}>
                      <Typography variant="body2">
                        <strong>Name:</strong> {validationResult.manifest.name}
                      </Typography>
                      <Typography variant="body2">
                        <strong>Version:</strong>{" "}
                        {validationResult.manifest.version}
                      </Typography>
                      <Typography variant="body2">
                        <strong>Title:</strong>{" "}
                        {validationResult.manifest.title}
                      </Typography>
                    </Box>
                  )}
                </Alert>
              ) : (
                <Alert severity="error">
                  Validation failed:
                  <ul style={{ margin: "8px 0 0 0", paddingLeft: "20px" }}>
                    {validationResult.errors?.map(
                      (error: string, idx: number) => (
                        <li key={idx}>{error}</li>
                      ),
                    )}
                  </ul>
                </Alert>
              )}
            </Box>
          )}
        </Box>
      </Paper>
    </Box>
  );
};

export default InstallModule;
