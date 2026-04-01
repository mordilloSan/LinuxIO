import { useEffect, useEffectEvent, useRef } from "react";

export function useDismissibleLayer<T extends HTMLElement>(
  open: boolean,
  onClose: () => void,
) {
  const layerRef = useRef<T>(null);

  const handlePointerDown = useEffectEvent((event: MouseEvent | TouchEvent) => {
    const target = event.target;

    if (!(target instanceof Node)) {
      return;
    }

    if (!layerRef.current?.contains(target)) {
      onClose();
    }
  });

  const handleKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if (event.key === "Escape") {
      onClose();
    }
  });

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("touchstart", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("touchstart", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  return layerRef;
}
