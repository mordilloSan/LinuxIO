import { useCallback, useState } from "react";

export function useDialogPresence<T>(value: T | null) {
  const [content, setContent] = useState(value);

  if (value !== null && content !== value) {
    setContent(value);
  }

  const onExited = useCallback(() => {
    if (value === null) setContent(null);
  }, [value]);

  return { content, onExited } as const;
}
