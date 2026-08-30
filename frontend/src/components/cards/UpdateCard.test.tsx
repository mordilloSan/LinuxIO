import { describe, expect, it, vi } from "vitest";

import type { Update } from "@/api";
import UpdateCard from "@/components/cards/UpdateCard";
import { render, screen } from "@/test/render";

const deferredUpdate: Update = {
  changelog: "",
  cve: [],
  info_enum: 9,
  issued: "",
  package_id: "demo;1.0;amd64;repo",
  restart: 0,
  state: 0,
  summary: "Demo update",
  version: "1.0",
};

describe("UpdateCard", () => {
  it("keeps deferred updates visible while disabling only their Update action", async () => {
    const onUpdate = vi.fn();
    const onPrefetchChangelog = vi.fn();
    const onToggleChangelog = vi.fn();
    const { user } = render(
      <UpdateCard
        changelog={undefined}
        isCurrentPackage={false}
        isExpanded={false}
        isLoadingChangelog={false}
        isUpdating={false}
        onPrefetchChangelog={onPrefetchChangelog}
        onToggleChangelog={onToggleChangelog}
        onUpdate={onUpdate}
        update={deferredUpdate}
      />,
    );

    const availabilityChip = screen.getByText("Available later");
    expect(availabilityChip).toBeInTheDocument();
    expect(screen.getByText("Demo update")).toBeInTheDocument();
    expect(screen.getByText("demo · 1.0")).toBeInTheDocument();
    expect(
      screen.getByText(/commonly happens during phased rollouts/i),
    ).toBeInTheDocument();
    const updateAction = screen.getByRole("button", { name: "Update" });
    expect(updateAction).toBeDisabled();
    await user.click(updateAction);
    expect(onUpdate).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "View Changelog" }));
    expect(onToggleChangelog).toHaveBeenCalledTimes(1);

    await user.hover(screen.getByRole("button", { name: "View Changelog" }));
    expect(onPrefetchChangelog).toHaveBeenCalledTimes(1);
  });
});
