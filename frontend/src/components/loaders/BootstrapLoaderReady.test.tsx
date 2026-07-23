import { useEffect, type ReactElement } from "react";
import { afterEach, describe, expect, it } from "vitest";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import PageLoader from "@/components/loaders/PageLoader";
import { render, waitFor } from "@/test/render";

const renderWithBootstrapLoader = (ui: ReactElement) => {
  const bootstrapLoader = document.createElement("div");
  bootstrapLoader.id = "app-bootstrap-loader";
  document.body.append(bootstrapLoader);

  const root = document.createElement("div");
  root.id = "root";
  root.setAttribute("aria-busy", "true");
  root.setAttribute("inert", "");
  document.body.append(root);

  return {
    bootstrapLoader,
    ...render(ui, { container: root }),
  };
};

describe("BootstrapLoaderReady", () => {
  afterEach(() => {
    document.getElementById("app-bootstrap-loader")?.remove();
    document.getElementById("root")?.remove();
  });

  it("removes the bootstrap loader when the route is ready", async () => {
    const { container } = renderWithBootstrapLoader(<BootstrapLoaderReady />);

    await waitFor(() =>
      expect(document.getElementById("app-bootstrap-loader")).toBeNull(),
    );
    expect(container).not.toHaveAttribute("aria-busy");
    expect(container).not.toHaveAttribute("inert");
  });

  it("waits for the initial page loader to finish", async () => {
    const { bootstrapLoader, rerender } = renderWithBootstrapLoader(
      <>
        <PageLoader />
        <BootstrapLoaderReady />
      </>,
    );

    expect(bootstrapLoader).toBeInTheDocument();

    rerender(<BootstrapLoaderReady />);

    await waitFor(() => expect(bootstrapLoader).not.toBeInTheDocument());
  });

  it("keeps the bootstrap loader when loading starts in an effect", async () => {
    function EffectLoader() {
      useEffect(() => {
        const pageLoader = document.createElement("div");
        pageLoader.className = "page-loader";
        document.getElementById("root")?.append(pageLoader);
        return () => pageLoader.remove();
      }, []);
      return null;
    }

    const { bootstrapLoader, rerender } = renderWithBootstrapLoader(
      <>
        <EffectLoader />
        <BootstrapLoaderReady />
      </>,
    );

    await waitFor(() =>
      expect(document.querySelector(".page-loader")).toBeInTheDocument(),
    );
    expect(bootstrapLoader).toBeInTheDocument();

    rerender(<BootstrapLoaderReady />);

    await waitFor(() => expect(bootstrapLoader).not.toBeInTheDocument());
  });
});
