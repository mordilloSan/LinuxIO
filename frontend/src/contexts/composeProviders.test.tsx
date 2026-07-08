import { render, screen } from "@testing-library/react";
import { useEffect, type PropsWithChildren } from "react";
import { describe, expect, it } from "vitest";

import { ComposeProviders, withProps } from "./composeProviders";

let mountLog: string[] = [];

function makeTracked(name: string) {
  return function Tracked({ children }: PropsWithChildren) {
    useEffect(() => {
      mountLog.push(`mount:${name}`);
      return () => {
        mountLog.push(`unmount:${name}`);
      };
    }, []);
    return (
      <div data-name={name} data-testid={name}>
        {children}
      </div>
    );
  };
}

function Labelled({ children, label }: PropsWithChildren<{ label?: string }>) {
  return <div data-testid="labelled" data-label={label}>{children}</div>;
}

describe("ComposeProviders", () => {
  it("renders entries first-to-last as outermost-to-innermost", () => {
    render(
      <ComposeProviders providers={[makeTracked("outer"), makeTracked("inner")]}>
        <span data-testid="leaf" />
      </ComposeProviders>,
    );

    const outer = screen.getByTestId("outer");
    const inner = screen.getByTestId("inner");
    expect(outer.contains(inner)).toBe(true);
    expect(inner.contains(screen.getByTestId("leaf"))).toBe(true);
  });

  it("passes props to tuple-form entries", () => {
    render(
      <ComposeProviders providers={[[Labelled, { label: "config" }]]}>
        <span />
      </ComposeProviders>,
    );

    expect(screen.getByTestId("labelled")).toHaveAttribute(
      "data-label",
      "config",
    );
  });

  it("passes typed props through withProps", () => {
    render(
      <ComposeProviders providers={[withProps(Labelled, { label: "typed" })]}>
        <span />
      </ComposeProviders>,
    );

    expect(screen.getByTestId("labelled")).toHaveAttribute(
      "data-label",
      "typed",
    );
  });

  it("keeps provider state across rerenders that rebuild the providers array", () => {
    const Outer = makeTracked("outer");
    const Inner = makeTracked("inner");
    mountLog = [];

    function Harness({ tick }: { tick: number }) {
      return (
        <ComposeProviders providers={[Outer, Inner]}>
          <span>{tick}</span>
        </ComposeProviders>
      );
    }

    const { rerender } = render(<Harness tick={1} />);
    rerender(<Harness tick={2} />);

    expect(mountLog).toEqual(["mount:inner", "mount:outer"]);
  });

  it("remounts only the keyed entry's subtree when its key changes", () => {
    const Above = makeTracked("above");
    const Keyed = makeTracked("keyed");
    const Below = makeTracked("below");
    mountLog = [];

    function Harness({ userId }: { userId: string }) {
      return (
        <ComposeProviders
          providers={[Above, withProps(Keyed, { key: userId }), Below]}
        >
          <span />
        </ComposeProviders>
      );
    }

    const { rerender } = render(<Harness userId="alice" />);
    mountLog = [];
    rerender(<Harness userId="bob" />);

    expect(mountLog).not.toContain("unmount:above");
    expect(mountLog).toContain("unmount:keyed");
    expect(mountLog).toContain("unmount:below");
    expect(mountLog).toContain("mount:keyed");
    expect(mountLog).toContain("mount:below");
  });

  it("renders children directly when the providers list is empty", () => {
    render(
      <ComposeProviders providers={[]}>
        <span data-testid="leaf" />
      </ComposeProviders>,
    );

    expect(screen.getByTestId("leaf")).toBeInTheDocument();
  });
});
