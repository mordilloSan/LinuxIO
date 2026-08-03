import { describe, expect, it, vi } from "vitest";

import AppChip from "@/components/ui/AppChip";
import { render, screen } from "@/test/render";

describe("AppChip", () => {
  it.each([
    ["Enter", "{Enter}"],
    ["Space", " "],
  ])("activates on %s", async (_label, key) => {
    const onClick = vi.fn();
    const { user } = render(<AppChip label="Active" onClick={onClick} />);
    const chip = screen.getByRole("button", { name: "Active" });

    chip.focus();
    await user.keyboard(key);

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("does not activate while disabled", async () => {
    const onClick = vi.fn();
    const { user } = render(
      <AppChip disabled label="Inactive" onClick={onClick} />,
    );
    const chip = screen.getByRole("button", { name: "Inactive" });

    chip.focus();
    await user.keyboard("{Enter}");
    await user.click(chip);

    expect(onClick).not.toHaveBeenCalled();
  });
});
