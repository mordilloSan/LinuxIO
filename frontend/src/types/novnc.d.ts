declare module "@novnc/novnc" {
  export interface RFBOptions {
    credentials?: Record<string, string>;
    focusOnClick?: boolean;
    shared?: boolean;
    viewOnly?: boolean;
    wsProtocols?: string[];
  }

  export default class RFB extends EventTarget {
    constructor(
      target: HTMLElement,
      // URL to connect to, or a WebSocket-like channel object (e.g. StreamMessageChannel).
      urlOrChannel: string | object,
      options?: RFBOptions,
    );

    clipViewport: boolean;
    focusOnClick: boolean;
    resizeSession: boolean;
    scaleViewport: boolean;
    showDotCursor: boolean;
    viewOnly: boolean;

    disconnect(): void;
  }
}
