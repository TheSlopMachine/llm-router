# Backend Architecture

Single-binary OpenAI-compatible routing gateway: Go backend, embedded Svelte
SPA, embedded bbolt DB. `docs/PLUGIN-API.md` is the binding plugin contract;
this file maps the Go core.

## System map

```
cmd/root.go              CLI entrypoint (--web/--api/--db)
internal/server/         HTTP server: dashboard (38080), /v1 API (38081)
internal/services/
  token/                 router-token issue/validate
  router/                ModelId → backend + CredentialPool, single pass, no repeats
  provider/              ProviderInstance CRUD (all types, one path)
  credential/            credential pool, usage stats
  virtual/               virtual models (fall-through lists + instruction)
  luaplugin/             Lua execution core: manifest, sandbox, HTTP+SSRF, storage
  pluginrepo/            plugin store: single-URL index repos (repo URL or direct index.json, files resolved against the index directory); code-defined built-in repos (`BuiltinRepos`, seeded on startup, protected from removal)
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets, 90d retention
  maintenance/           refresh + cleanup (refresh, modelsync, proxy, auth jobs)
  exhausted/             joint limit keys (account/model/proxy), subset match, expiry auto-delete
internal/pool/           single-pass credential failover (unary + stream)
internal/streamgate/     first-byte gate: failover stops after first SSE byte
internal/httpkit/        shared transport helpers (SSE headers)
internal/errors/         domain sentinels + MapUpstream + ToAPIError
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/models, /v1/messages
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go)
providers/virtual/       built-in virtual-models backend (Go)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
scripts/smoke/           black-box smoke harness + mock provider (testdata/mock.lua)
docs/                    PLUGIN-API.md (binding plugin contract), BACKEND.md (core map), CHANGELOG.md
Makefile                 thin launcher for scripts/ — see AGENTS.md §3
```

Keep changes shallow. Touch service internals only when the task requires it.

## Request flow

`Parse → Resolve → Disabled → IsModelEnabled → checkEndpoint → capability
→ loadCredentials → invoke → dropMissingModel(not_found) → metrics.`

- `router/route.go:resolveRequest` owns the shared pipeline. Endpoint code
  adds only virtual short-circuit, capability pre-check, pool call.
- Virtual models fan out through the router re-entrantly. The outer token
  rules travel in ctx (`router/service.go:withTokenRules`); inner member
  calls inherit them. `nil` token with no snapshot stays unrestricted
  (admin probes only).
- Token credential scopes (`models.CredentialScope`) union with the legacy
  flat `allowed_credentials`: scope `{provider, all}` covers future keys.

## Pool invariants

- `pool.Run / pool.RunStream`: one attempt per key, pool order, no repeats,
  no backoff. Fatal errors (`ErrHandlerNotFound`, `invalid_request`) stop
  immediately.
- Proxy source keys qualify per plugin (`<recordID>/<name>`);
  `proxypool.RekeySource` migrates legacy bare tags once at startup.
- `streamgate.Writer`: failover continues only before the first byte reaches
  the client. After that the stream belongs to one upstream.
- Usage tracking is best-effort but never silent: failures log with the
  credential ID. Limit state lives only in `exhausted.Service`: rate/quota
  outcomes mark the scoped joint key (or the full combination without
  scope) in the exec defer; nothing else writes limit state.

## Exhausted store

- Stored keys act as filters over candidate dimensions (plugin, provider
  type, account, model, proxy). A candidate matching every stored dimension
  skips until `ResetsAt` passes.
- Credential pools drop matching combinations before the token filter; an
  all-limited pool stays as last resort. Proxy picks filter after ranking;
  manual mode with nothing usable left fails loudly.
- Expired entries delete on read; `Prune` sweeps the rest. `geo`, `auth`
  and other non-rate types never mark.

## Error contract

- Domain errors live in `internal/errors`: sentinels (`ErrNotFound`,
  `ErrTimeout`, `ErrRateLimited`, …) + `models.ProviderError` with a typed
  `ErrorType`. No `strings.Contains` matching anywhere.
- `MapUpstream(status, code, type, message)`: exact status/code matching
  from the upstream envelope; message text never decides (except quota
  wording on bare 429s in the Lua classify helper).
- `ToAPIError(err)`: the single domain→wire table for both surfaces.
  `ErrorTypeForCode` maps wire codes to OpenAI error types.
- Provider 404 (`ErrorTypeNotFound`) → wire `not_found`, evicts the model
  from the info cache on every routed path.

## Transactions

One public method = one `db.Update` when the operation must be atomic:
`token.Create/Delete/Regenerate` (token + index), `provider.Delete`
(provider + credentials), `credential.Reorder/DeleteByProvider`,
metrics batch persist. `repository` returns `ErrNotFound` via `%w`;
services map it with `errors.Is`, never by string.

## Lifecycle

- `metrics.Stop`: workers join, `eventCh` drains, all in-memory buckets
  flush (`flushAll`, no age cutoff). Periodic aggregation persists only
  buckets older than 1h and evicts from memory after durable writes.
- `maintenance.Start(ctx)`: one goroutine, one tick, independent jobs
  (credential refresh, model sync, proxy rotation/fetch, auth cleanup).
  A credential-list failure never skips model sync. Proxy fetch state is
  per source; one failing source never delays the others.
- Constructors perform no I/O. Legacy migration runs in `EnsureSeeded`,
  where failures surface as errors.

## Buckets

`internal/db` owns bucket names and creation. Removed buckets (`agents`,
`proxy_limits`) drop at startup; legacy rows migrate explicitly
(`migrateLegacyCustom` inside `EnsureSeeded`, `migrateDropProxyLimits` /
`migrateClearCredentialQuota` in `server`); migrations never silently
discard user data.

## Smoke harness

`scripts/smoke/` drives the wire surfaces black-box against a dev stack
(`make smoke` restarts with `NO_AUTH=1` first): status → bootstrap →
plugin install → provider + dev-database credentials → model matrix
(one model per capability, first success closes it) → cleanup of created
credentials. Quota, payment, rate and missing-model outcomes skip with
reason; anything else fails. Exit 0 means clean (skips allowed).

## Adding an endpoint

1. `models`: `Endpoint*` constant + `SupportsEndpoint` coverage.
2. `provider`: capability interface (`Transcriber`-style) for Go backends.
3. `luaplugin`: `Handler*` constant in `handler_names.go` (+ sandbox
   registration + `callAndDecode` wiring in `decode.go`).
4. `router`: resolve + capability + pool call (see `route.go`).
5. `api/v1`: handler + `authorizeModel` + `recordRouteMetric`.
6. Plugin contract: extend `docs/PLUGIN-API.md` in the same change.
