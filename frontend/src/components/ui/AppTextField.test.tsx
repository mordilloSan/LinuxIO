import { describe, expect, it } from "vitest";

import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppTextField from "@/components/ui/AppTextField";
import { render, screen } from "@/test/render";

describe("AppTextField", () => {
  it("passes an accessible label to the native input", () => {
    render(<AppTextField aria-label="Search services" type="search" />);

    expect(
      screen.getByRole("searchbox", { name: "Search services" }),
    ).toBeInTheDocument();
  });

  it("associates a visible label with the input when no id is given", () => {
    render(<AppTextField label="Mount point" />);

    expect(screen.getByLabelText("Mount point")).toBeInTheDocument();
  });

  it("keeps a caller-supplied id", () => {
    render(<AppTextField id="custom-id" label="Name" />);

    expect(screen.getByLabelText("Name")).toHaveAttribute("id", "custom-id");
  });

  it("labels the AppAutocomplete input", () => {
    render(
      <AppAutocomplete label="Filesystem" options={["ext4", "xfs"]} value="" />,
    );

    expect(screen.getByLabelText("Filesystem")).toBeInTheDocument();
  });
});
