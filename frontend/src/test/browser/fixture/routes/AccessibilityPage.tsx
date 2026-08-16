import { useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";

import "@/routes/_authenticated/-components/dock/dock.css";

export default function AccessibilityPage() {
  const [activations, setActivations] = useState({
    button: 0,
    chip: 0,
    icon: 0,
  });
  const activate = (control: keyof typeof activations) =>
    setActivations((current) => ({
      ...current,
      [control]: current[control] + 1,
    }));

  return (
    <main style={{ minHeight: "200vh", padding: "32px" }}>
      <h1>Accessibility fixture</h1>
      <p>Use Tab to move through each interactive control.</p>
      <div style={{ display: "flex", gap: "16px", alignItems: "center" }}>
        <AppButton
          aria-label="Activate button"
          onClick={() => activate("button")}
        >
          Button
        </AppButton>
        <AppIconButton
          aria-label="Activate icon button"
          onClick={() => activate("icon")}
        >
          <span aria-hidden="true">✦</span>
        </AppIconButton>
        <AppChip
          color="primary"
          label="Activate chip"
          onClick={() => activate("chip")}
        />
      </div>
      <output aria-live="polite" data-testid="activation-counts">
        Button: {activations.button}; Icon: {activations.icon}; Chip:{" "}
        {activations.chip}
      </output>
      <a className="app-dock-link" data-testid="dock-settings" href="#settings">
        <span className="app-dock__label">Settings</span>
      </a>
      <div style={{ height: "1600px" }} />
    </main>
  );
}
