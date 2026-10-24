import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import type { ComposeTask } from "./composeProgress";
import DockerComposeProgress from "./DockerComposeProgress";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

describe("DockerComposeProgress", () => {
  it("exposes group disclosure state and controls its layer rows", async () => {
    const tasks: ComposeTask[] = [
      {
        id: "Image alpine",
        text: "Pulling",
        status: "Working",
        pct: 50,
      },
      {
        id: "layer-1",
        parent_id: "Image alpine",
        text: "Downloading",
        status: "Working",
        pct: 50,
        current: 5,
        total: 10,
      },
    ];

    const { user } = render(<DockerComposeProgress tasks={tasks} />);
    const trigger = screen.getByRole("button", { name: "alpine details" });
    const panelId = trigger.getAttribute("aria-controls");

    expect(panelId).toBeTruthy();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(document.getElementById(panelId ?? "")).toBeInTheDocument();

    await user.click(trigger);

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("layer-1")).toBeInTheDocument();
  });
});
