import { lazy } from "react";
import type React from "react";

type LazyRouteModule<T extends React.ComponentType<any>> = { default: T };
type LazyRouteImporter<T extends React.ComponentType<any>> = () => Promise<
  LazyRouteModule<T>
>;

export type PreloadableLazyRoute<T extends React.ComponentType<any>> =
  React.LazyExoticComponent<T> & {
    preload: LazyRouteImporter<T>;
  };

export function lazyWithPreload<T extends React.ComponentType<any>>(
  importer: LazyRouteImporter<T>,
): PreloadableLazyRoute<T> {
  let preloadPromise: Promise<LazyRouteModule<T>> | undefined;
  const preload = () => {
    preloadPromise ??= importer().catch((error) => {
      preloadPromise = undefined;
      throw error;
    });
    return preloadPromise;
  };

  const Component = lazy(preload) as PreloadableLazyRoute<T>;
  Component.preload = preload;
  return Component;
}

export function withRouteIcons<T extends React.ComponentType<any>>(
  importer: LazyRouteImporter<T>,
): LazyRouteImporter<T> {
  return () =>
    Promise.all([import("@/icons/icons"), importer()]).then(
      ([, routeModule]) => routeModule,
    );
}
