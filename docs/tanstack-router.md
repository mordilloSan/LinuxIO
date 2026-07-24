# TanStack Router architecture

Status: TanStack Router is the application's only routing path as of
2026-07-23.

## Route architecture

- Routes live in `frontend/src/routes/` and use `createFileRoute`.
- URL segments are real directories with a `route.tsx`, `index.tsx`, or
  `$.tsx` route file. Route-owned UI is co-located in `-components/` (and the
  dashboard in `-dashboard/`), using TanStack Router's native `-` ignore
  convention.
- `@tanstack/router-plugin` generates `routeTree.gen.ts` and automatically
  code-splits route components.
- `router.tsx` creates the typed application router from the generated tree.
- `routes/-provider.tsx` injects the live auth, capability, QueryClient, and
  update-blocking context into that router.
- `_authenticated.tsx` is the pathless authenticated layout. Its native
  `beforeLoad` guard runs before protected child loaders.
- Capability and privilege guards are route-local `beforeLoad` functions.
- Search parameters are validated by the route that owns them.
- Navigation and access metadata are route-local `staticData`; the sidebar
  reads that metadata from the router instead of maintaining another route
  catalog.
- File Browser uses the native `filebrowser/$` splat and `_splat` parameter.
- Unmatched authenticated URLs use the pathless layout's native
  `notFoundComponent`, preserving the authenticated shell. The root route owns
  the global not-found and error components.

The former code-based router factory, component registry, protected-route
catalog, loader registry, and lazy-component wrappers have been removed.

## Preloading and code splitting

The router has one application-wide policy:

```ts
defaultPreload: "intent",
defaultPreloadDelay: 150,
defaultPreloadStaleTime: 0,
```

Links inherit this policy. There are no per-link or per-route preload delays,
and no `250 ms` override. `defaultPreloadStaleTime: 0` lets TanStack Query,
rather than the router cache, decide whether route data is fresh.

The Vite router plugin runs before the React plugin with
`autoCodeSplitting: true`. Route components, pending UI, error UI, and
not-found UI are lazy boundaries managed by TanStack Router. Critical route
configuration—search validation, guards, loaders, and static data—remains
available for matching and intent preloading.

## Route loader inventory

Every protected page that owns initial route data has a co-located loader:

| Route | Initial loader data |
| --- | --- |
| Authenticated shell | application version shown in the persistent footer |
| Dashboard | health, host, uptime, time, CPU, memory, filesystems, network, motherboard, GPU, drives, disk throughput, and Docker summaries when available |
| Network | network interfaces |
| Updates | available updates and history when the history tab is active |
| Services | active unit list for the selected section and selected-unit details |
| Logs | request-transport readiness; service filter data loads in background |
| Storage | disks/filesystems/NFS mounts, or LVM physical volumes, volume groups, and logical volumes |
| Docker | data for the active tab; auto-update state only for Containers |
| VMs | VM list, preflight status, and the initially selected VM detail |
| Accounts | active users/groups list and selected-user details/login history |
| Shares | NFS/Samba shares or NFS/CIFS mounts, according to the active tab |
| WireGuard | WireGuard interfaces |
| Hardware | sensors, PCI devices, memory modules, hardware summaries, and monitoring histories when available |
| Navigator | the resource for the current splat path |
| Terminal | request-transport readiness before the stream-only page mounts |

Search-dependent data is declared through `loaderDeps`, so a relevant search
change reruns the loader with a distinct dependency identity. Each query loader
returns the result of generated endpoint `queryOptions` loaded through the
router's shared QueryClient. Request failures propagate to the route error
boundary. The mounted `useQuery` observer consumes and subscribes to those same
cache entries instead of starting an independent initial request.

Sign-in, not-found, and root routes own no backend data and therefore do not
contain artificial no-op loaders. Dialog-only and click-selected data stays
with its enabled query rather than being fetched speculatively for every route
visit.

## Update boundary

While a live application update blocks requests, sidebar entries are inert and
the navigation blocker rejects transitions. Route query loading checks the live
update state before transport readiness and again immediately before the
request. Intent-prefetched queries are tagged silent and speculative so an
unreliable hover preload does not create a global error toast.

## Regression coverage

- `router.test.tsx` checks the generated topology, the single
  global preload policy, loader coverage, route-local navigation metadata, and
  access metadata.
- `routes/-loader.test.tsx` checks shared-cache reuse, request
  readiness, update races, and speculative metadata.
- `routes/_authenticated/-components/sidebar/useSidebarItems.test.tsx` checks
  access filtering, static-data
  ordering, and the absence of per-link preload overrides.
- `routes/-provider.test.tsx` checks bootstrap gating and live
  router-context invalidation.

`routeTree.gen.ts` is generated code. It is committed for TypeScript consumers
but excluded from formatting.


Resposta curta:
Migração de React Router para TanStack Router: sim, está concluída.
Todos os padrões 100% idiomáticos TanStack Router + Query: ainda não.
Todas as rotas com dados próprios têm loader: sim.
Todo dado crítico segue loader → Query cache → Suspense: ainda existem alguns casos híbridos.
A documentação recomenda loader + ensureQueryData + useSuspenseQuery para dados críticos da primeira renderização. É exatamente o padrão predominante atual, mas ainda não é universal. TanStack External Data Loading
O que está finalizado
A estrutura Router está correta:
Zero dependências ou imports de React Router.
TanStack Router montado em produção.
File-based routing com autoCodeSplitting.
Não existe a antiga pasta pages.
Uma única declaração de Register.
Contexto tipado com auth, access e QueryClient.
Guards em beforeLoad.
Search params e loaderDeps.
ensureQueryData partilhando exatamente a mesma cache dos componentes.
defaultPreload: "intent".
defaultPreloadStaleTime: 0, recomendado quando a frescura é controlada pelo TanStack Query. TanStack preloading
Pending e error components globais.
queryOptions e query keys centralizados.
Não existem wrappers .useQuery.
Existem 17 ficheiros de definição de rota:
14 rotas de aplicação têm loader.
As únicas sem loader são __root, _authenticated e sign-in, porque não têm dados remotos próprios.
Logs e Terminal têm loaders de transporte, porque os dados principais chegam por streams.
Dados críticos ainda sem Suspense
Estes são os casos que impedem dizer “100% finalizado”:
Updates
O loader carrega get_updates_basic, mas [UpdatesPage.tsx (line 33)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/updates/-components/UpdatesPage.tsx:33) ainda consome com useQuery.
Deveria existir um componente renderizado apenas quando PackageKit está disponível e usar useSuspenseQuery.
Shares
O loader carrega NFS/Samba apenas no tab Shares, corretamente. Contudo, [SharesPage.tsx (line 1168)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/shares/-components/SharesPage.tsx:1168) consome ambas com useQuery.
O padrão mais idiomático seria separar SharesTab e MountsTab; cada componente ativo consumiria os seus dados com Suspense.
Hardware history inicial
O loader carrega os quatro histories de 1h, mas [HardwareHistoryCards.tsx (line 496)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx:496) usa useQuery.
Depois da alteração de range, useQuery + placeholderData + polling está correto. O estado inicial é híbrido: ou é crítico e deve usar Suspense, ou deixa de pertencer ao loader.
Docker dashboard toolbar
[DockerPage.tsx (line 58)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/docker/-components/DockerPage.tsx:58) observa list_containers com useQuery para as ações globais do dashboard, embora o loader já carregue os containers.
Não é um fetch duplicado graças à mesma query key, mas continua a ser um consumidor crítico não-Suspense.
Docker auto-update
[useContainerAutoUpdateState.ts (line 51)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/docker/-components/useContainerAutoUpdateState.ts:51) usa useQuery e está montado acima de todos os tabs.
Isto foi deliberado para não bloquear todos os tabs. Para ficar totalmente limpo, o controller deveria ser montado apenas no tab Containers ou quando o diálogo é aberto.
VM selecionada
O loader carrega o detalhe da primeira VM, mas [VMPage.tsx (line 76)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/vm/-components/VMPage.tsx:76) usa useQuery para a VM selecionada.
Como a seleção é estado React local, não pode ser migrado cegamente para Suspense. O padrão Router mais completo seria colocar a VM selecionada no search param, adicioná-la a loaderDeps e carregar o respetivo detalhe no loader.
WireGuard Create
A rota e o dashboard usam loader + Suspense para list_interfaces, mas [CreateInterfaceButton.tsx (line 28)](/home/miguelmariz/LinuxIO/frontend/src/routes/_authenticated/wireguard/-components/CreateInterfaceButton.tsx:28) volta a observar a mesma query com useQuery.
Além disso, network_info começa imediatamente, embora só seja necessário para abrir o diálogo. Não bloqueia a rota, mas deveria ser lazy ou receber os interfaces já carregados.
Onde não usamos loader/Suspense intencionalmente
Estes casos estão corretos com useQuery, useQueries ou fetch imperativo:
WireGuard peers depois de selecionar uma interface.
QR code depois de selecionar um peer.
Changelog quando se expande um update.
NFS/CIFS remote browsing depois de introduzir um servidor.
VM preflight dependente dos campos do diálogo.
Shells, groups e users necessários em diálogos de Accounts.
Date/time settings quando o diálogo abre.
Failed-login events quando o painel abre.
Terminal shells quando o diálogo abre.
Filebrowser search, details, stat, editor, subfolders e directory sizes.
Docker icons carregados individualmente e mantidos em cache.
Footer version, power status, monitoring e indexer settings.
Logs live stream e Terminal stream.
Fetches imperativos usados em ações, validação, backfill de gráficos e resolução de paths.
Suspense não é obrigatório para queries lazy. Aliás, useSuspenseQuery não suporta enabled, tornando useQuery o padrão correto nesses casos. TanStack lazy queries
Outra pendência: error boundaries antigas
A boundary TanStack global existe, mas ainda temos [ErrorBoundary.tsx (line 15)](/home/miguelmariz/LinuxIO/frontend/src/components/errors/ErrorBoundary.tsx:15) dentro de:
Todos os tabs através de [TabPanel.tsx (line 16)](/home/miguelmariz/LinuxIO/frontend/src/components/tabbar/TabPanel.tsx:16).
Dashboard widgets.
Hardware cards.
Settings e Footer.
Essa boundary pode capturar erros lançados por useSuspenseQuery antes de chegarem à RouteError, mas não integra useQueryErrorResetBoundary. Portanto, error handling ainda não está verdadeiramente centralizado.
Também [RouteError.tsx (line 8)](/home/miguelmariz/LinuxIO/frontend/src/routes/-components/RouteError.tsx:8) faz reset ao clicar em retry, mas ainda não segue completamente o lifecycle recomendado pela documentação, que também reseta a Query boundary quando a error component monta.
Outra pendência: tabs/search
[useTabUrlState.ts (line 25)](/home/miguelmariz/LinuxIO/frontend/src/hooks/useTabUrlState.ts:25) usa:
useSearch({ strict: false });
uma lista manual de nomes de search params;
valores de tab validados apenas como string.
Funciona com TanStack Router, mas não é o padrão mais fortemente tipado. Para ficar totalmente idiomático, cada rota deve validar os valores possíveis do tab e consumir Route.useSearch()/Route.useNavigate() de forma estrita.
Conclusão: o cutover do Router está concluído e a arquitetura geral está correta. A migração estritamente idiomática ainda precisa de uma última fase pequena: resolver os sete consumidores híbridos, substituir/integrar as boundaries antigas e tornar os tabs route-typed.
