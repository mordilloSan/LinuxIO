import type { CallDefinition } from "./call-react-query";
import type {
  CommandInput,
  CommandName,
  CommandProgress,
  CommandRequest,
  CommandResult,
  CallRoute,
  HandlerName,
} from "./generated/linuxio-types";
import type { RouteModeFor } from "./generated/route-metadata";
import type { ProgressFrame } from "./StreamMultiplexer";
import type { TaskEndpoint } from "./task-react-query";

type DeclaredCommandProgress<
  H extends HandlerName,
  C extends CommandName<H>,
> = [CommandProgress<H, C>] extends [never]
  ? ProgressFrame
  : CommandProgress<H, C>;

type DeclaredRoute<H extends HandlerName, C extends CommandName<H>> = Extract<
  `${Extract<H, string>}.${Extract<C, string>}`,
  CallRoute
>;

type DeclaredCommandEndpoint<
  H extends HandlerName,
  C extends CommandName<H>,
> = `${Extract<H, string>}.${Extract<C, string>}` extends CallRoute
  ? CallDefinition<DeclaredRoute<H, C>>
  : RouteModeFor<`${Extract<H, string>}.${Extract<C, string>}`> extends "task"
    ? TaskEndpoint<
        CommandInput<H, C>,
        CommandRequest<H, C>,
        CommandResult<H, C>,
        DeclaredCommandProgress<H, C>
      >
    : never;

type HandlerEndpoints<H extends HandlerName> = {
  [C in CommandName<H>]: DeclaredCommandEndpoint<H, C>;
};

/** Aggregate type for the generated endpoint namespace. */
export type TypedAPI = {
  [H in HandlerName]: HandlerEndpoints<H>;
};
