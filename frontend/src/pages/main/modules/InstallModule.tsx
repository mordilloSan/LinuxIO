import React, { useState } from "react";
import { toast } from "sonner";

import linuxio from "@/api/react-query";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppPaper from "@/components/ui/AppPaper";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
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

  const { mutate: validateMutation, isPending: validatePending } =
    linuxio.modules.validate_module.useMutation();

  const { mutate: installMutation, isPending: installPending } =
    linuxio.modules.install_module.useMutation();

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
        setPath("");
        setTargetName("");
        setCreateSymlink(false);
        setValidationResult(null);
        onInstalled();
      },
    });
  };

  return (
    <div style={{ marginTop: 8, maxWidth: 800 }}>
      <AppPaper style={{ padding: 12 }}>
        <AppTypography variant="h6" gutterBottom>
          Install Module from Path
        </AppTypography>
        <AppTypography
          variant="body2"
          color="text.secondary"
          style={{ marginBottom: 8 }}
        >
          Install a module from a local filesystem path. The module must contain
          a valid <code>module.yaml</code> file.
        </AppTypography>

        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <AppTextField
            label="Module Path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/path/to/module"
            fullWidth
            helperText="Absolute path to the module directory"
          />

          <AppTextField
            label="Target Name (Optional)"
            value={targetName}
            onChange={(e) => setTargetName(e.target.value)}
            placeholder="Leave empty to use module's name"
            fullWidth
            helperText="Custom name for the installed module"
          />

          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={createSymlink}
                onChange={(e) => setCreateSymlink(e.target.checked)}
              />
            }
            label="Create symlink (for development)"
          />

          <div style={{ display: "flex", gap: 8 }}>
            <AppButton
              variant="outlined"
              onClick={handleValidate}
              disabled={validatePending || installPending || !path}
            >
              {validatePending ? <AppCircularProgress size={20} /> : "Validate"}
            </AppButton>
            <AppButton
              variant="contained"
              onClick={handleInstall}
              disabled={validatePending || installPending || !path}
            >
              {installPending ? <AppCircularProgress size={20} /> : "Install"}
            </AppButton>
          </div>

          {validationResult && (
            <div style={{ marginTop: 8 }}>
              {validationResult.valid ? (
                <AppAlert severity="success">
                  Module is valid!
                  {validationResult.manifest && (
                    <div style={{ marginTop: 4 }}>
                      <AppTypography variant="body2">
                        <strong>Name:</strong> {validationResult.manifest.name}
                      </AppTypography>
                      <AppTypography variant="body2">
                        <strong>Version:</strong>{" "}
                        {validationResult.manifest.version}
                      </AppTypography>
                      <AppTypography variant="body2">
                        <strong>Title:</strong>{" "}
                        {validationResult.manifest.title}
                      </AppTypography>
                    </div>
                  )}
                </AppAlert>
              ) : (
                <AppAlert severity="error">
                  Validation failed:
                  <ul style={{ margin: "8px 0 0 0", paddingLeft: "20px" }}>
                    {validationResult.errors?.map(
                      (error: string, idx: number) => (
                        <li key={idx}>{error}</li>
                      ),
                    )}
                  </ul>
                </AppAlert>
              )}
            </div>
          )}
        </div>
      </AppPaper>
    </div>
  );
};

export default InstallModule;
