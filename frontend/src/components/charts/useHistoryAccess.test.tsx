import { describe, expect, it } from "vitest";

import { render, screen } from "@/test/render";

import { useHistoryAccess } from "./HistoryCard";

const Probe = () => {
  const { isEnabled, reason } = useHistoryAccess();
  return (
    <output>
      {String(isEnabled)}:{reason ?? ""}
    </output>
  );
};

describe("useHistoryAccess", () => {
  it("reads history only in a session with sudo", () => {
    const view = render(<Probe />, {
      auth: { privileged: true },
      capabilities: { monitoringAvailable: true },
    });
    expect(screen.getByRole("status")).toHaveTextContent(/^true:/);
    view.unmount();

    render(<Probe />, {
      auth: { privileged: false },
      capabilities: { monitoringAvailable: true },
    });
    expect(screen.getByRole("status")).toHaveTextContent(
      "false:Historical monitoring requires a privileged session.",
    );
  });
});
