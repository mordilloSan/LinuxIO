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
      urlOrChannel: string | unknown,
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
