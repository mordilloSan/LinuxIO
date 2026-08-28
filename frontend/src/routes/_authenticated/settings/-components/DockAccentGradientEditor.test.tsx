import { describe, expect, it, vi } from "vitest";

import { resolveDockAccentGradient } from "@/routes/_authenticated/-components/dock/dockPalette";
import { fireEvent, render, screen } from "@/test/render";
import buildAppTheme from "@/theme";
import { toHexColor } from "@/utils/color";

import DockAccentGradientEditor from "./DockAccentGradientEditor";

describe("DockAccentGradientEditor", () => {
  it("names both color endpoints and marks the kept palette tiles", () => {
    const value = {
      startColor: "",
      endColor: "",
      rangeStart: 10,
      rangeEnd: 90,
    };
    render(<DockAccentGradientEditor onChange={() => {}} value={value} />);

    // With no stored endpoints the editor shows the accent family derived
    // from the rendered theme's primary colour.
    const accent = buildAppTheme("DARK").palette.primary.main;
    const family = resolveDockAccentGradient(accent, value);
    expect(
      screen.getByLabelText("Start color for the full dock gradient"),
    ).toHaveValue(toHexColor(family.startColor));
    expect(
      screen.getByLabelText("End color for the full dock gradient"),
    ).toHaveValue(toHexColor(family.endColor));

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
