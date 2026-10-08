import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import ModelMenu, { type SelectOption } from "./ModelMenu";

const model: SelectOption = {
  id: "model",
  name: "Model",
  type: "select",
  category: "model",
  currentValue: "opus",
  options: [
    { value: "opus", name: "Opus 5.5", description: "Most capable" },
    { value: "sonnet", name: "Sonnet 5.5", description: "Fast and capable" },
  ],
};

const effort: SelectOption = {
  id: "effort",
  name: "Effort",
  type: "select",
  category: "thought_level",
  currentValue: "high",
  options: [
    { value: "low", name: "Low" },
    { value: "high", name: "High" },
  ],
};

function setup(props: Partial<Parameters<typeof ModelMenu>[0]> = {}) {
  const onChange = vi.fn();
  const box = document.createElement("div");
  box.getBoundingClientRect = () =>
    ({
      width: 640,
      height: 80,
      top: 0,
      left: 0,
      right: 640,
      bottom: 80,
    }) as DOMRect;
  document.body.append(box);
  render(
    <ModelMenu
      disabled={false}
      effort={effort}
      model={model}
      onChange={onChange}
      widthAnchor={box}
      {...props}
    />,
  );
  return { onChange, box };
}

describe("ModelMenu", () => {
  it("shows model and effort on one chip", () => {
    setup();
    const chip = screen.getByRole("button", { name: "Model: Opus 5.5 High" });
    expect(chip).toHaveTextContent("Opus 5.5High");
    expect(within(chip).getByText("High")).toHaveClass("assistant__pill-minor");
  });

  it("opens one menu as wide as the prompt with model descriptions", async () => {
    setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    const menu = await screen.findByRole("menu", { name: "Model" });
    const paper = menu.closest(".app-popover__paper") as HTMLElement;
    expect(paper).toHaveClass("assistant__model-menu");
    expect(paper.style.width).toBe("640px");
    expect(within(menu).getByText("Most capable")).toBeInTheDocument();
    expect(within(menu).getByText("Fast and capable")).toBeInTheDocument();
    expect(within(menu).getByText("Effort")).toBeInTheDocument();
    expect(
      within(menu).getByRole("menuitem", { name: /Opus 5\.5/ }),
    ).toHaveClass("app-menu__item--selected");
  });

  it("picking a model closes the menu", async () => {
    const { onChange } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /Sonnet 5\.5/ }),
    );
    expect(onChange).toHaveBeenCalledWith("model", "sonnet");
    expect(screen.queryByRole("menu", { name: "Model" })).toBeNull();
  });

  it("shows effort as a slider with the level name, sending once when the drag ends", async () => {
    const { onChange } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    const slider = await screen.findByRole("slider", { name: "Effort" });
    expect(slider.closest(".assistant__effort")).toHaveTextContent(
      "Effort (High)",
    );
    expect(slider).toHaveAttribute("aria-valuetext", "High");
    // Both levels up to the thumb carry the accent.
    const effortRow = slider.closest(".assistant__effort") as HTMLElement;
    expect(
      effortRow.querySelectorAll(".assistant__effort-dot--on"),
    ).toHaveLength(2);

    // Dragging moves the thumb and the label but sends nothing yet.
    fireEvent.change(slider, { target: { value: "0" } });
    expect(onChange).not.toHaveBeenCalled();
    expect(slider.closest(".assistant__effort")).toHaveTextContent(
      "Effort (Low)",
    );

    fireEvent.pointerUp(slider);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith("effort", "low");
    // The menu stays open so the model can be changed in the same visit.
    expect(screen.getByRole("menu", { name: "Model" })).toBeInTheDocument();
  });

  it("does not send when the slider ends where it started", async () => {
    const { onChange } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    const slider = await screen.findByRole("slider", { name: "Effort" });
    fireEvent.change(slider, { target: { value: "0" } });
    fireEvent.change(slider, { target: { value: "1" } });
    fireEvent.pointerUp(slider);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("keeps arrow keys on the slider instead of moving menu focus", async () => {
    setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    const slider = await screen.findByRole("slider", { name: "Effort" });
    slider.focus();
    fireEvent.keyDown(slider, { key: "ArrowDown" });
    expect(slider).toHaveFocus();
  });

  it("shows model settings as rows above the effort slider", async () => {
    const { onChange } = setup({
      extras: [
        {
          id: "fast",
          name: "Fast mode",
          description: "Faster responses on supported models",
          type: "boolean",
          category: "model_config",
          currentValue: true,
        },
      ],
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    const row = await screen.findByRole("menuitem", { name: "Fast mode, on" });
    expect(row).toHaveTextContent("Faster responses on supported models");
    expect(row.querySelector(".assistant__toggle--on")).not.toBeNull();
    // The settings sit in the pinned footer, before the effort slider.
    const footer = row.closest(".assistant__model-footer") as HTMLElement;
    expect(footer).not.toBeNull();
    expect(footer.querySelector(".assistant__effort")).not.toBeNull();
    await userEvent.click(row);
    expect(onChange).toHaveBeenCalledWith("fast", false);
    // Toggling keeps the menu open.
    expect(screen.getByRole("menu", { name: "Model" })).toBeInTheDocument();
  });

  it("cycles a select-shaped model setting from older agents", async () => {
    const { onChange } = setup({
      extras: [
        {
          id: "fast",
          name: "Fast mode",
          type: "select",
          category: "model_config",
          currentValue: "off",
          options: [
            { value: "on", name: "On" },
            { value: "off", name: "Off" },
          ],
        },
      ],
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    );
    const row = await screen.findByRole("menuitem", { name: /Fast mode/ });
    expect(row).toHaveTextContent("Off");
    await userEvent.click(row);
    expect(onChange).toHaveBeenCalledWith("fast", "on");
  });

  it("works with only an effort option", () => {
    setup({ model: undefined });
    expect(
      screen.getByRole("button", { name: "Model: High" }),
    ).toBeInTheDocument();
  });

  it("is disabled while connecting", () => {
    setup({ disabled: true });
    expect(
      screen.getByRole("button", { name: "Model: Opus 5.5 High" }),
    ).toBeDisabled();
  });
});
