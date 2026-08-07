import type { FileRouteTypes } from "@/routeTree.gen";

export type RouteTarget =
  | {
      params?: never;
      to: Exclude<FileRouteTypes["to"], "/filebrowser/$">;
    }
  | {
      params: { _splat: string };
      to: "/filebrowser/$";
    };

export type ToastMeta =
  | (RouteTarget & {
      label?: string;
    })
  | {
      label?: string;
      params?: never;
      to?: never;
    };
