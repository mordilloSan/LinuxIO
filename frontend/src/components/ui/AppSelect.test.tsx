import { describe, expect, it } from "vitest";

import AppSelect from "@/components/ui/AppSelect";
import { fireEvent, render, screen } from "@/test/render";

describe("AppSelect accessibility", () => {
  it("binds the open combobox to its live listbox and preserves expanded state", () => {
    render(
      <AppSelect value="one">
        <option value="one">One</option>
        <option value="two">Two</option>
      </AppSelect>,
    );

    const combobox = screen.getByRole("combobox");
    expect(combobox).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(combobox);

    const listbox = screen.getByRole("listbox");
    expect(combobox).toHaveAttribute("aria-expanded", "true");
    expect(listbox).toHaveAttribute("id");
    expect(combobox).toHaveAttribute(
      "aria-controls",
      listbox.getAttribute("id"),
    );

    fireEvent.click(combobox);
    expect(combobox).toHaveAttribute("aria-expanded", "false");
    expect(combobox).not.toHaveAttribute("aria-controls");
  });
});
