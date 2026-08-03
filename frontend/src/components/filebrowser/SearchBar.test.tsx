import { describe, expect, it } from "vitest";

import { render, screen } from "@/test/render";

import SearchBar from "./SearchBar";

describe("SearchBar", () => {
  it("disables the clear action with the field", () => {
    render(<SearchBar disabled onChange={() => {}} value="query" />);

    expect(screen.getByRole("button", { name: "clear search" })).toBeDisabled();
  });
});
