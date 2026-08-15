import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import AppHeaderSearch from "./AppHeaderSearch";

describe("AppHeaderSearch", () => {
  it("reports changes and clears the current search", async () => {
    const onChange = vi.fn();
    const { user } = render(
      <AppHeaderSearch
        onChange={onChange}
        placeholder="Search services..."
        value="ssh"
      />,
    );

    await user.type(screen.getByLabelText("Search services..."), "d");
    expect(onChange).toHaveBeenCalledWith("sshd");

    await user.click(screen.getByRole("button", { name: "Clear search" }));
    expect(onChange).toHaveBeenLastCalledWith("");
  });

  it("disables the clear action with the field", () => {
    render(<AppHeaderSearch disabled onChange={() => {}} value="query" />);

    expect(screen.getByRole("button", { name: "Clear search" })).toBeDisabled();
  });
});
