import { describe, expect, it } from "vitest";

import AppAlert from "@/components/ui/AppAlert";
import { render, screen } from "@/test/render";

describe("AppAlert roles", () => {
  it("is announced as an alert by default", () => {
    render(<AppAlert severity="error">Could not save.</AppAlert>);

    expect(screen.getByRole("alert")).toHaveTextContent("Could not save.");
  });

  it("takes a note role for a warning that is there from the start", () => {
    render(
      <AppAlert role="note" severity="warning">
        Saving replaces them.
      </AppAlert>,
    );

    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("note")).toHaveTextContent("Saving replaces them.");
  });
});
