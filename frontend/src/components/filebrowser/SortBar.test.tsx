import type { HTMLAttributes } from "react";
import { describe, expect, it, vi } from "vitest";

import SortBar from "@/components/filebrowser/SortBar";
import { fireEvent, render, screen } from "@/test/render";

vi.mock("@iconify/react", () => ({
  Icon: ({
    icon,
    ...props
  }: HTMLAttributes<HTMLSpanElement> & { icon: string }) => (
    <span {...props} data-icon={icon} />
  ),
}));

describe("SortBar", () => {
  it("exposes the active sort and direction and reveals direction on focus", () => {
    const onSortChange = vi.fn();
    render(
      <SortBar onSortChange={onSortChange} sortField="size" sortOrder="desc" />,
    );

    const size = screen.getByRole("button", {
      name: "Sort by Size, descending",
    });
    expect(size).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("sort-icon-size")).toBeInTheDocument();

    const name = screen.getByRole("button", { name: "Sort by Name" });
    expect(name).toHaveAttribute("aria-pressed", "false");
    fireEvent.focus(name);
    expect(screen.getByTestId("sort-icon-name")).toBeInTheDocument();
    fireEvent.click(name);
    expect(onSortChange).toHaveBeenCalledWith("name");
  });
});
