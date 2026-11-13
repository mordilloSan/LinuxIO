import { describe, expect, it } from "vitest";

import PageLoader from "@/components/loaders/PageLoader";
import WidgetLoader from "@/components/loaders/WidgetLoader";
import { render, screen } from "@/test/render";

describe("PageLoader", () => {
  it("announces a generic loading status by default", () => {
    render(<PageLoader />);
    expect(screen.getByRole("status", { name: "Loading" })).toBeInTheDocument();
  });

  it("announces what is loading when WidgetLoader passes a label", () => {
    render(<WidgetLoader label="Starting Claude Code" />);
    expect(
      screen.getByRole("status", { name: "Starting Claude Code" }),
    ).toBeInTheDocument();
  });
});
