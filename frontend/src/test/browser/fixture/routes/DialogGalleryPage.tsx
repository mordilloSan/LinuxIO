import { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFullscreenDialog from "@/components/ui/AppFullscreenDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";

import "@fontsource-variable/inter/wght.css";

export default function DialogGalleryPage() {
  const [dialog, setDialog] = useState<
    "form" | "fullscreen" | "confirm" | "pending" | null
  >(null);
  const [name, setName] = useState("wg1");
  const [dns, setDns] = useState("");
  const [nic, setNic] = useState("eth0");
  const [user, setUser] = useState("alice");
  const close = () => setDialog(null);
  const fields = (
    <div className="app-dialog-fields">
      <AppTextField
        fullWidth
        label="Interface name"
        onChange={(event) => setName(event.target.value)}
        value={name}
      />
      <AppSelect
        fullWidth
        label="NIC"
        onChange={(event) => setNic(event.target.value)}
        value={nic}
      >
        <option value="eth0">eth0 (172.29.51.224/20)</option>
        <option value="eth1">eth1 (192.168.1.10/24)</option>
      </AppSelect>
      <AppTextField
        fullWidth
        label="DNS (optional)"
        onChange={(event) => setDns(event.target.value)}
        value={dns}
      />
      <AppTextField
        error
        fullWidth
        helperText="Choose an unused port and try again."
        label="Port"
        readOnly
        size="small"
        value="51821"
      />
      <AppSelect fullWidth label="Protocol" size="small" value="udp">
        <option value="udp">UDP</option>
      </AppSelect>
      <AppAutocomplete
        fullWidth
        label="Owner"
        onChange={setUser}
        options={["alice", "bob"]}
        size="small"
        value={user}
      />
    </div>
  );

  return (
    <main style={{ padding: "var(--app-space-24)" }}>
      <AppButton onClick={() => setDialog("form")}>Open form</AppButton>
      <AppButton onClick={() => setDialog("fullscreen")}>
        Open fullscreen
      </AppButton>
      <AppButton onClick={() => setDialog("confirm")}>
        Open confirmation
      </AppButton>
      <AppButton onClick={() => setDialog("pending")}>
        Open pending confirmation
      </AppButton>
      <AppTextField
        label="Outside dialog"
        value="Default field size"
        readOnly
      />
      <GeneralDialog
        fullWidth
        maxWidth="xs"
        onClose={close}
        open={dialog === "form"}
      >
        <form
          className="app-dialog-form"
          onSubmit={(event) => {
            event.preventDefault();
            close();
          }}
        >
          <AppDialogTitle>Configure interface</AppDialogTitle>
          <AppDialogContent>{fields}</AppDialogContent>
          <AppDialogActions>
            <AppButton onClick={close}>Cancel</AppButton>
            <AppButton type="submit" variant="contained">
              Save changes
            </AppButton>
          </AppDialogActions>
        </form>
      </GeneralDialog>
      <AppFullscreenDialog onClose={close} open={dialog === "fullscreen"}>
        <AppDialogTitle>Fullscreen settings</AppDialogTitle>
        <AppDialogContent>{fields}</AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={close}>Close</AppButton>
        </AppDialogActions>
      </AppFullscreenDialog>
      <UnsavedChangesDialog
        onDiscardAndExit={close}
        onKeepEditing={close}
        onSaveAndExit={close}
        open={dialog === "confirm"}
      />
      <ConfirmDialog
        destructive
        isPending
        title="Delete items"
        message="Removing selected files."
        onClose={close}
        onConfirm={close}
        open={dialog === "pending"}
      />
    </main>
  );
}
