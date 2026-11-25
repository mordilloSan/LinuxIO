import { useNavigate } from "@tanstack/react-router";
import { useEffect, useSyncExternalStore } from "react";

import { assistantStore } from "./assistant-store";

const openChat = () => assistantStore.getState().sessionId;

/**
 * Keeps the page's address naming the open chat (`/assistant?chat=<id>`),
 * whoever opened it: the Chats card, New chat, a delete, the newest on
 * connect. The address is replaced, not pushed, so Back leaves the page
 * instead of stepping through chats it may not be able to switch to mid-turn.
 * A chat the address names that is not the open one, such as an older history
 * entry, is rewritten rather than loaded: only a connect reads it
 * (`assistantStore.setRequestedChat`).
 */
export function useChatAddress(chat: string | undefined) {
  const navigate = useNavigate();
  const open = useSyncExternalStore(assistantStore.subscribe, openChat);
  useEffect(() => {
    if (open && open !== chat) {
      void navigate({
        to: "/assistant",
        search: { chat: open },
        replace: true,
        resetScroll: false,
      });
    }
  }, [open, chat, navigate]);
}
