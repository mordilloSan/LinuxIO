import { afterEach, describe, expect, it, vi } from "vitest";

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

  it("forwards an aria-label to the interactive trigger", () => {
    render(
      <AppSelect aria-label="History range" value="one">
        <option value="one">One</option>
      </AppSelect>,
    );

    expect(screen.getByRole("combobox")).toHaveAccessibleName("History range");
  });

  it("lists options inside an optgroup under a group heading", () => {
    render(
      <AppSelect value="b">
        <optgroup label="Fast">
          <option value="a">A</option>
        </optgroup>
        <optgroup label="Smart">
          <option value="b">B</option>
        </optgroup>
      </AppSelect>,
    );

    const combobox = screen.getByRole("combobox");
    expect(combobox).toHaveTextContent("B");
    fireEvent.click(combobox);
    expect(screen.getAllByRole("option")).toHaveLength(2);
    expect(screen.getByText("Fast")).toBeInTheDocument();
    expect(screen.getByText("Smart")).toBeInTheDocument();
  });
});

describe("AppSelect Escape", () => {
  it("claims Escape while the list is open and leaves it alone otherwise", () => {
    render(
      <AppSelect value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    const combobox = screen.getByRole("combobox");
    expect(fireEvent.keyDown(combobox, { key: "Escape" })).toBe(true);
    fireEvent.click(combobox);
    expect(fireEvent.keyDown(combobox, { key: "Escape" })).toBe(false);
    expect(combobox).toHaveAttribute("aria-expanded", "false");
  });
});

describe("AppSelect placement", () => {
  afterEach(() => vi.restoreAllMocks());

  const rectAt = (top: number, bottom: number) =>
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
      top,
      bottom,
      left: 10,
      right: 110,
      width: 100,
      height: bottom - top,
      x: 10,
      y: top,
      toJSON: () => ({}),
    });

  it("opens upward when the viewport bottom would clip the list", () => {
    rectAt(window.innerHeight - 30, window.innerHeight - 10);
    render(
      <AppSelect value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    fireEvent.click(screen.getByRole("combobox"));
    const list = screen.getByRole("listbox");
    expect(list.style.bottom).toBe("32px");
    expect(list.style.top).toBe("auto");
  });

  it("opens downward when there is room", () => {
    rectAt(10, 30);
    render(
      <AppSelect value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    fireEvent.click(screen.getByRole("combobox"));
    const list = screen.getByRole("listbox");
    expect(list.style.top).toBe("32px");
    expect(list.style.bottom).toBe("");
  });

  it("always opens upward with placement top, even with room below", () => {
    rectAt(10, 30);
    render(
      <AppSelect placement="top" value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    fireEvent.click(screen.getByRole("combobox"));
    const list = screen.getByRole("listbox");
    expect(list.style.bottom).toBe(`${window.innerHeight - 10 + 2}px`);
    expect(list.style.top).toBe("auto");
  });

  it("keeps at least 120px of height when opening downward", () => {
    rectAt(40, window.innerHeight - 50);
    render(
      <AppSelect value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    fireEvent.click(screen.getByRole("combobox"));
    // 42px below, 32px above: stays downward, floored at 120px.
    expect(screen.getByRole("listbox").style.maxHeight).toBe("120px");
  });

  it("floors the height at 120px with placement top near the viewport top", () => {
    rectAt(40, 60);
    render(
      <AppSelect placement="top" value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    fireEvent.click(screen.getByRole("combobox"));
    expect(screen.getByRole("listbox").style.maxHeight).toBe("120px");
  });

  it("falls back to downward in auto when there is under 120px above", () => {
    // 2px below, 52px above: neither fits, so the downward floor applies.
    rectAt(60, window.innerHeight - 10);
    render(
      <AppSelect value="one">
        <option value="one">One</option>
      </AppSelect>,
    );
    fireEvent.click(screen.getByRole("combobox"));
    const list = screen.getByRole("listbox");
    expect(list.style.top).toBe(`${window.innerHeight - 10 + 2}px`);
    expect(list.style.maxHeight).toBe("120px");
  });
});
