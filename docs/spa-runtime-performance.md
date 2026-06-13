# SPA Runtime Performance Checklist

This is the working checklist for the SPA runtime-performance cleanup.

- [x] Initial bundle: split the Iconify registry so auth/shell/sidebar icons are separate from route-specific icons.
- [x] Authenticated runtime: split `AuthGuard` into a lightweight gate plus lazy authenticated providers.
- [ ] Dashboard network card: remove render-time state updates and derive or effect-sync selected interface state.
- [x] Global fonts/icons: remove Space Grotesk and Material Icons; use the local breadcrumb home icon.
- [x] Route preload: keep intent-based preload only, with no all-route background preload.
- [ ] Migrate routing to TanStack Router.
