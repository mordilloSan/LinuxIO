import { describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen } from "@/test/render";

import DockAccentGradientEditor from "./DockAccentGradientEditor";

describe("DockAccentGradientEditor", () => {
  it("names both color endpoints and marks the kept palette tiles", () => {
    render(
      <DockAccentGradientEditor
        accent="#2196f3"
        onChange={() => {}}
        value={{
          startColor: "",
          endColor: "",
          rangeStart: 10,
          rangeEnd: 90,
        }}
      />,
    );

    expect(
      screen.getByLabelText("Start color for the full dock gradient"),
    ).toHaveValue("#21f3e7");
    expect(
      screen.getByLabelText("End color for the full dock gradient"),
    ).toHaveValue("#212df3");

    const tiles = screen.getAllByRole("button", { name: /^Palette stop / });
    expect(tiles).toHaveLength(11);
    expect(
      screen.getByRole("button", { name: "Palette stop 0%" }),
    ).toHaveAttribute("aria-pressed", "false");
    expect(
      screen.getByRole("button", { name: "Palette stop 10%" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByRole("button", { name: "Palette stop 90%" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByRole("button", { name: "Palette stop 100%" }),
    ).toHaveAttribute("aria-pressed", "false");
  });

  it("emits complete values when an endpoint or range changes", () => {
    const onChange = vi.fn();
    render(
      <DockAccentGradientEditor
        accent="#2196f3"
        onChange={onChange}
        value={{
          startColor: "#ff0000",
          endColor: "#0000ff",
          rangeStart: 20,
          rangeEnd: 80,
        }}
      />,
    );

    fireEvent.change(
      screen.getByLabelText("Start color for the full dock gradient"),
      { target: { value: "#00ff00" } },
    );
    expect(onChange).toHaveBeenLastCalledWith({
      startColor: "#00ff00",
      endColor: "#0000ff",
      rangeStart: 20,
      rangeEnd: 80,
    });
  });

  it("moves the nearest range boundary to the clicked tile", async () => {
    const onChange = vi.fn();
    const { user } = render(
      <DockAccentGradientEditor
        accent="#2196f3"
        onChange={onChange}
        value={{
          startColor: "#ff0000",
          endColor: "#0000ff",
          rangeStart: 20,
          rangeEnd: 80,
        }}
      />,
    );

    // Inside the range and closer to the start boundary.
    await user.click(screen.getByRole("button", { name: "Palette stop 40%" }));
    expect(onChange).toHaveBeenLastCalledWith({
      startColor: "#ff0000",
      endColor: "#0000ff",
      rangeStart: 40,
      rangeEnd: 80,
    });

    // Outside the range, past the end boundary.
    await user.click(screen.getByRole("button", { name: "Palette stop 90%" }));
    expect(onChange).toHaveBeenLastCalledWith({
      startColor: "#ff0000",
      endColor: "#0000ff",
      rangeStart: 20,
      rangeEnd: 90,
    });

    // Outside the range, before the start boundary.
    await user.click(screen.getByRole("button", { name: "Palette stop 0%" }));
    expect(onChange).toHaveBeenLastCalledWith({
      startColor: "#ff0000",
      endColor: "#0000ff",
      rangeStart: 0,
      rangeEnd: 80,
    });
  });

  it("resets custom colors and range back to the live accent family", async () => {
    const onChange = vi.fn();
    const { user } = render(
      <DockAccentGradientEditor
        accent="#2196f3"
        onChange={onChange}
        value={{
          startColor: "#ff0000",
          endColor: "#0000ff",
          rangeStart: 20,
          rangeEnd: 80,
        }}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Reset" }));

    expect(onChange).toHaveBeenCalledWith({
      startColor: "",
      endColor: "",
      rangeStart: 0,
      rangeEnd: 100,
    });
  });
});
