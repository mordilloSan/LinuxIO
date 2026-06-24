import { describe, expect, it, vi } from "vitest";

import AppButton from "@/components/ui/AppButton";
import { render, screen } from "@/test/render";

describe("AppButton", () => {
  it("defaults to a non-submit button and passes through props", async () => {
    const onClick = vi.fn();
    const { user } = render(<AppButton onClick={onClick}>Run</AppButton>);
    const button = screen.getByRole("button", { name: "Run" });

    expect(button).toHaveAttribute("type", "button");
    await user.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
