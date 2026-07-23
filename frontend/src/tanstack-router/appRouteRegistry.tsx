import {
  AuthenticatedLayout,
  Page404,
  protectedRouteComponents,
  SignIn,
} from "@/routes";
import { protectedRouteLoaders } from "@/routing/protectedRouteLoaders";

import { createTanStackRouter, type LinuxIORouterContext } from "./router";

export function createApplicationRouter(context: LinuxIORouterContext) {
  return createTanStackRouter({
    components: {
      AuthenticatedLayout,
      NotFound: Page404,
      ProtectedRoutes: protectedRouteComponents,
      SignIn,
    },
    context,
    protectedRouteLoaders,
  }).router;
}
