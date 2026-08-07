import { afterEach, describe, expect, it } from "vitest";

import { AppDialog } from "@/components/ui/AppDialog";
import AppFullscreenDialog from "@/components/ui/AppFullscreenDialog";
import { render } from "@/test/render";

interface HarnessProps {
  appOpen: boolean;
  fullscreenOpen: boolean;
}

function Harness({ appOpen, fullscreenOpen }: HarnessProps) {
  return (
    <>
      <AppDialog open={appOpen}>Dialog</AppDialog>
      <AppFullscreenDialog open={fullscreenOpen}>
        Fullscreen
      </AppFullscreenDialog>
    </>
  );
}

describe("dialog body scroll lock", () => {
  afterEach(() => {
    document.body.style.overflow = "";
  });

  it("keeps the body locked when the first of two standard dialogs closes", () => {
    document.body.style.overflow = "auto";
    const view = render(
      <>
        <AppDialog open>First dialog</AppDialog>
        <AppDialog open>Second dialog</AppDialog>
      </>,
    );

    view.rerender(
      <>
        <AppDialog open={false}>First dialog</AppDialog>
        <AppDialog open>Second dialog</AppDialog>
      </>,
    );
    expect(document.body.style.overflow).toBe("hidden");

    view.rerender(
      <>
        <AppDialog open={false}>First dialog</AppDialog>
        <AppDialog open={false}>Second dialog</AppDialog>
      </>,
    );
    expect(document.body.style.overflow).toBe("auto");
  });

  it("keeps the body locked while mixed overlays remain open after non-LIFO closure", () => {
    document.body.style.overflow = "auto";
    const view = render(<Harness appOpen fullscreenOpen />);

    expect(document.body.style.overflow).toBe("hidden");
    view.rerender(<Harness appOpen={false} fullscreenOpen />);
    expect(document.body.style.overflow).toBe("hidden");

    view.rerender(<Harness appOpen={false} fullscreenOpen={false} />);
    expect(document.body.style.overflow).toBe("auto");
  });

  it("restores the original inline overflow only after the last overlay closes", () => {
    document.body.style.overflow = "scroll";
    const view = render(<Harness appOpen fullscreenOpen />);

    view.rerender(<Harness appOpen fullscreenOpen={false} />);
    expect(document.body.style.overflow).toBe("hidden");

    view.rerender(<Harness appOpen={false} fullscreenOpen={false} />);
    expect(document.body.style.overflow).toBe("scroll");
  });
});
