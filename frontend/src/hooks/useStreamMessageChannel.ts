import { useCallback, useEffect, useRef } from "react";

import {
  createStreamMessageChannel,
  type Stream,
  type StreamMessageChannel,
  type StreamMessageChannelOptions,
} from "@/api";

export interface UseStreamMessageChannelReturn {
  /** Close the current channel, if any. */
  closeChannel: () => void;
  /**
   * Adapt a stream into a message channel, closing any previously opened
   * one. Returns the channel for synchronous consumers (noVNC's RFB takes
   * it in its constructor).
   */
  openChannel: (
    stream: Stream,
    options?: StreamMessageChannelOptions,
  ) => StreamMessageChannel;
}

/**
 * Lifecycle owner for a `StreamMessageChannel` — the WebSocket-like adapter
 * that message-event consumers such as noVNC require. The hook owns
 * open/close/unmount cleanup so pages never touch the channel primitive.
 */
export function useStreamMessageChannel(): UseStreamMessageChannelReturn {
  const channelRef = useRef<StreamMessageChannel | null>(null);

  const closeChannel = useCallback(() => {
    channelRef.current?.close();
    channelRef.current = null;
  }, []);

  const openChannel = useCallback(
    (stream: Stream, options?: StreamMessageChannelOptions) => {
      closeChannel();
      const channel = createStreamMessageChannel(stream, options);
      channelRef.current = channel;
      return channel;
    },
    [closeChannel],
  );

  useEffect(() => closeChannel, [closeChannel]);

  return { openChannel, closeChannel };
}
