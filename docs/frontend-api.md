# Frontend API

The frontend API is documented in the canonical [API Contract](./api-contract.md).

Start there for:

- generated `linuxio` endpoint shape
- `frontend/src/api` file map
- request/result typing rules
- query, mutation, job, and stream usage
- adding a new endpoint

Feature code should import from `@/api` and should not edit files under `frontend/src/api/generated`.
