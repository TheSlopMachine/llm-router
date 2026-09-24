# Backend Architecture

Single-binary OpenAI-compatible routing gateway: Go backend, embedded Svelte
SPA, embedded bbolt DB. `docs/PLUGIN-API.md` is the binding plugin contract;
this file maps the Go core.

## System map

```
cmd/root.go → config.Config → server.New → db (bbolt)
                                    ↓
provider.Service (registry + Resolve + EnsureSeeded)
  ↑ RegisterGoAdapter ← generic.Adapter (custom) | virtual.Adapter
  ↑ SetLuaService   ← luaplugin.Service (VM, handlers, pool, manifest)
  ↓
credential.Service (pool All/SortPool) + token.Service (Rules)
  + modelinfo.Service (cache + overrides, 1h TTL)
  ↓
router.Service: Parse(ModelId=provider/model) → Resolve → Disabled
  → model gate → endpoint gate → capability → loadCredentials
  → pool (single pass, first success wins, last error out)
  ↓
proxypool.Service (pick/probe/rate-limit) ← luaplugin httpclient
  ← router through the server-wired resolver
  ↓
HTTP: api/v1 (OpenAI-compatible) + dashboard (admin REST + SPA fallback)
  ↓
background: maintenance (refresh, modelsync, proxy, auth jobs)
  + metrics (1m buckets, 90d retention) + pluginrepo (store index)
  ↓
persistence: repository (generic buckets) + models (wire types)
  + errors (sentinels) + pool (failover) + streamgate (first-byte gate)
```

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
  no backoff. Fatal errors (`ErrHandlerNotFound`) stop immediately.
- Proxy source keys are qualified per plugin (`<recordID>/<name>`);
  `proxypool.RekeySource` migrates legacy bare tags once at startup.
- `streamgate.Writer`: failover continues only before the first byte reaches
  the client. After that the stream belongs to one upstream.
- Usage tracking is best-effort but never silent: failures log with the
  credential ID. Quota marks require `ErrorTypeQuotaExceeded + RetryAfter`.

## Error contract

- Domain errors live in `internal/errors`: sentinels (`ErrNotFound`,
  `ErrTimeout`, `ErrRateLimited`, …) + `models.ProviderError` with a typed
  `ErrorType`. No `strings.Contains` matching anywhere.
- `MapUpstream(status, code, type, message)`: exact status/code matching
  from the upstream envelope; message text never decides.
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

`internal/db` owns bucket names and creation. Removed buckets (`agents`)
drop in `initBuckets`. Legacy rows migrate explicitly (`migrateLegacyCustom`
inside `EnsureSeeded`); migrations never silently discard user data.

## Adding an endpoint

1. `models`: `Endpoint*` constant + `SupportsEndpoint` coverage.
2. `provider`: capability interface (`Transcriber`-style) for Go backends.
3. `luaplugin`: `Handler*` constant in `handler_names.go` (+ sandbox
   registration + `callAndDecode` wiring in `decode.go`).
4. `router`: resolve + capability + pool call (see `route.go`).
5. `api/v1`: handler + `authorizeModel` + `recordRouteMetric`.
6. Plugin contract: extend `docs/PLUGIN-API.md` in the same change.
