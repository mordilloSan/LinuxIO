import { useLayoutEffect } from "react";

function BootstrapLoaderReady() {
  useLayoutEffect(() => {
    const bootstrapLoader = document.getElementById("app-bootstrap-loader");
    const appRoot = document.getElementById("root");
    if (!appRoot) return;

    let observer: MutationObserver | undefined;
    let removalTimer: number | undefined;
    const finish = () => {
      bootstrapLoader?.remove();
      appRoot.removeAttribute("aria-busy");
      appRoot.removeAttribute("inert");
      observer?.disconnect();
    };
    const removeWhenReady = () => {
      window.clearTimeout(removalTimer);
      if (appRoot.querySelector(".page-loader")) return;

      removalTimer = window.setTimeout(() => {
        if (!appRoot.querySelector(".page-loader")) finish();
      }, 0);
    };

    observer = new MutationObserver(removeWhenReady);
    observer.observe(appRoot, { childList: true, subtree: true });
    removeWhenReady();

    return () => {
      window.clearTimeout(removalTimer);
      observer?.disconnect();
    };
  }, []);

  return null;
}

export default BootstrapLoaderReady;
