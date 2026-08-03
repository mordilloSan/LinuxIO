import { describe, expect, it } from "vitest";

import AppTextField from "@/components/ui/AppTextField";
import { render, screen } from "@/test/render";

describe("AppTextField", () => {
  it("passes an accessible label to the native input", () => {
    render(<AppTextField aria-label="Search services" type="search" />);

    expect(
      screen.getByRole("searchbox", { name: "Search services" }),
    ).toBeInTheDocument();
  });
});
