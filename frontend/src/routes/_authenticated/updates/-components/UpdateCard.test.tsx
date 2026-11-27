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
    const { user } = render(
      <UpdateCard onUpdate={onUpdate} selected update={deferredUpdate} />,
    );

    const availabilityChip = screen.getByText("Available later");
    expect(availabilityChip).toBeInTheDocument();
    expect(screen.getByText("demo")).toBeInTheDocument();
    expect(screen.getByText("Demo update")).toBeInTheDocument();
    expect(screen.getByText("1.0", { exact: true })).toBeInTheDocument();
    expect(
      screen.getByText(/commonly happens during phased rollouts/i),
    ).toBeInTheDocument();
    const updateAction = screen.getByRole("button", { name: "Update" });
    expect(updateAction).toBeDisabled();
    await user.click(updateAction);
    expect(onUpdate).not.toHaveBeenCalled();
  });

  it("opens its details as a whole and holds no action of its own in the list", async () => {
    const onOpen = vi.fn();
    const onPrefetchChangelog = vi.fn();
    const { user } = render(
      <UpdateCard
        onOpen={onOpen}
        onPrefetchChangelog={onPrefetchChangelog}
        onUpdate={vi.fn()}
        update={{ ...deferredUpdate, info_enum: 11 }}
      />,
    );

    const opener = screen.getByRole("button", {
      name: "Open update demo details",
    });
    expect(screen.getAllByRole("button")).toEqual([opener]);

    await user.hover(screen.getByText("demo"));
    expect(onPrefetchChangelog).toHaveBeenCalledTimes(1);
    await user.click(opener);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("truncates a long summary with an ellipsis inside the card", () => {
    render(
      <UpdateCard
        onOpen={vi.fn()}
        update={{ ...deferredUpdate, summary: "A long summary" }}
      />,
    );

    const subtitle = screen.getByText("A long summary");
    // An inline span would let the text run past the card's padding.
    expect(subtitle.tagName).toBe("DIV");
    expect(subtitle).toHaveClass("app-typo--nowrap");
  });
});
