import { describe, expect, it, vi } from "vitest";

import type { Update } from "@/api";
import { render, screen } from "@/test/render";

import UpdateCard from "./UpdateCard";

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
    expect(screen.getByText("demo")).toBeInTheDocument();
    expect(screen.getByText("1.0 · Demo update")).toBeInTheDocument();
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

  it("links to the project when the update has no changelog", () => {
    render(
      <UpdateCard
        changelog="No changelog is available for this update"
        homepage="https://www.docker.com"
        isCurrentPackage={false}
        isExpanded
        isLoadingChangelog={false}
        isUpdating={false}
        onPrefetchChangelog={vi.fn()}
        onToggleChangelog={vi.fn()}
        onUpdate={vi.fn()}
        update={{ ...deferredUpdate, info_enum: 11 }}
      />,
    );

    expect(
      screen.getByText("No changelog is available for this update"),
    ).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Project page" });
    expect(link).toHaveAttribute("href", "https://www.docker.com");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });
});
