import { describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen } from "@/test/render";

import DockAccentGradientEditor from "./DockAccentGradientEditor";

describe("DockAccentGradientEditor", () => {
  it("names both color endpoints and both range handles", () => {
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
    const startRange = screen.getByRole("slider", {
      name: "Start of dock palette range",
    });
    const endRange = screen.getByRole("slider", {
      name: "End of dock palette range",
    });
    expect(startRange).toHaveValue("10");
    expect(endRange).toHaveValue("90");
    expect(startRange).toHaveAttribute("min", "0");
    expect(startRange).toHaveAttribute("max", "100");
    expect(endRange).toHaveAttribute("min", "0");
    expect(endRange).toHaveAttribute("max", "100");
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
    onChange.mockClear();

    const startRange = screen.getByRole("slider", {
      name: "Start of dock palette range",
    });
    fireEvent.change(startRange, { target: { value: "35" } });
    expect(onChange).not.toHaveBeenCalled();

    fireEvent.pointerUp(startRange);
    expect(onChange).toHaveBeenLastCalledWith({
      startColor: "#ff0000",
      endColor: "#0000ff",
      rangeStart: 35,
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
