import { screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import { render } from "@/test/render";

const Harness = ({
  clearOnDocumentEscape = false,
}: {
  clearOnDocumentEscape?: boolean;
}) => {
  const [value, setValue] = useState("linuxio");

  return (
    <AppHeaderSearch
      aria-label="Search"
      clearOnDocumentEscape={clearOnDocumentEscape}
      onChange={setValue}
      value={value}
    />
  );
};

describe("AppHeaderSearch escape handling", () => {
  it("clears on Escape and keeps focus while the field is focused", async () => {
    const { user } = render(<Harness />);
    const input = screen.getByRole("textbox", { name: "Search" });

    await user.click(input);
    await user.keyboard("{Escape}");

    expect(input).toHaveValue("");
    expect(input).toHaveFocus();
  });

  it("clears on a document Escape without taking focus when opted in", async () => {
    const { user } = render(<Harness clearOnDocumentEscape />);
    const input = screen.getByRole("textbox", { name: "Search" });

    await user.keyboard("{Escape}");

    expect(input).toHaveValue("");
    expect(input).not.toHaveFocus();
  });

  it("ignores a document Escape without the opt-in", async () => {
    const { user } = render(<Harness />);

    await user.keyboard("{Escape}");

    expect(screen.getByRole("textbox", { name: "Search" })).toHaveValue(
      "linuxio",
    );
  });

  it("leaves the field alone while a dialog is open", async () => {
    const dialogRoot = document.createElement("div");
    dialogRoot.className = "app-dialog-root";
    document.body.appendChild(dialogRoot);
    const { user } = render(<Harness clearOnDocumentEscape />);

    await user.keyboard("{Escape}");

    expect(screen.getByRole("textbox", { name: "Search" })).toHaveValue(
      "linuxio",
    );
    dialogRoot.remove();
  });
});
