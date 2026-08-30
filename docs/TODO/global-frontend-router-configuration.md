# Global frontend router configuration

Move these application-wide TanStack Router options out of
`frontend/src/router/router.tsx` and pass their effective values from the global
config file when creating the router:

```ts
defaultPreload: "intent",
defaultPreloadDelay: 50,
defaultPendingMs: 150,
defaultPendingMinMs: 0,
defaultPreloadStaleTime: 0,
```

Keep the config fields optional for existing installations. If the global
config file or any field is absent, use the exact value above for that field.
Keep this policy router-wide, update the config contract and generated types,
and replace the fixed-value router tests with coverage for both configured
values and the defaults.
