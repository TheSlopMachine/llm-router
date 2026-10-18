# Event Keys

Single source of truth for the metadata keys on persisted log events
(`GET /api/events`, `make log`). `make`-syntax examples run verbatim;
angle brackets hold a real id taken from a prior query. The `msg` example
needs direct `/api/events` access (see below). The plugin-side
vocabulary in `docs/PLUGIN-API.md` (logging convention) owns Lua key naming;
this document owns the full filterable key inventory, query mechanics, and
the Go-core keys no other document lists. A change that adds, renames, or
removes an event key updates this document in the same change.

## Record shape

One event carries `id`, `timestamp`, `severity` (0-4), `message`, and free-form
`keys` (`internal/services/events/service.go:62-68`). Services log
`snake_case` keys with full words; machine data never lives in message text,
so filtering always targets keys, never substrings.

## Query mechanics

- `severity` is a minimum: `0` debug, `1` info, `2` warn, `3` error, `4` fatal.
- `limit` defaults to 50 (`internal/services/events/service.go:29-30`).
- `filter.<key>[=<value>]` predicates match metadata exactly; a bare
  `filter.<key>` matches presence only
  (`internal/services/events/service.go:38-43`,
  `internal/dashboard/events.go:97-130`). There is no substring or pattern
  match: `filter.code=rate_limit` works, `filter.reason=quota` never matches
  `quota_exhausted`, and `filter.msg=` needs the full exact message.
- `exclude.<key>[=<value>]` predicates drop matching rows; a bare
  `exclude.<key>` drops presence only. Multiple exclusions unite: one match
  drops the row. Exclusions apply after inclusions.
- `EVENT_FILTERS` splits on whitespace, so values with spaces cannot pass
  through `make log`: query `/api/events` directly with URL-encoded values
  instead (`/api/events?filter.msg=rate%20limited&limit=50`). A leading `!`
  marks exclusion (`!event`, `!event=stream`).
- Entries below the configured `LOG_LEVEL` never persist
  (`internal/services/luaplugin/service.go:667-669`). Missing `debug` lines
  on an `info` stack are not evidence the path never ran.
- Retention is `EVENTS_RETENTION_DAYS` (default 3); older rows are pruned and
  cannot be queried back.

## Prefer stable keys

`code`, `model`, `type`, `provider_id`, `endpoint`, `operation`, `event`,
`cred_id` carry bounded values and make reliable filters. `reason`, `error`,
`detail`, `body`, `line`, `msg` carry variable text; use them to read, not
to filter, except for exact `msg=` matches against stable one-sentence
plugin messages.

## Request path (router + `/v1` compat)

Emitted by `internal/services/router/service.go` (`Routing request to ...`,
`... request fails`) and `internal/api/v1/compat_errors.go`
(`Request failed`, `Request validation failed`, `Request denied by token
rules`, `Request decode failed`, `Authentication failed`).

| Key | Meaning | Example filter |
|---|---|---|
| `provider_id` | Provider instance serving the call | `EVENT_FILTERS="provider_id=google"` |
| `type` | Plugin type key (`google`, `groq`, ...) | `EVENT_FILTERS="type=google"` |
| `model` | Full router model id (`provider/model`) | `EVENT_FILTERS="model=google/gemini-2.5-flash"` |
| `endpoint` | Request path (`chat/completions`, `audio/speech`, ...) | `EVENT_FILTERS="endpoint=audio/speech"` |
| `token_id` | Router token authorizing the call; empty on admin-listener and tokenless calls | `EVENT_FILTERS="token_id"` |
| `error` | Variable failure text | read, do not filter |
| `code` | Wire error code (`rate_limit`, `server_error`, `not_found`, `authentication_error`, `insufficient_quota`, ...) | `EVENT_FILTERS="code=rate_limit"` |
| `reason` | Variable machine reason (`unrestricted`, `provider_not_found`, ...) | read, do not filter |
| `operation` | Failing operation (`create_batch`, `test_credential`, ...) | `EVENT_FILTERS="operation=create_batch"` |
| `detail` | Record or resource id the operation acted on | `EVENT_FILTERS="detail=<batch-id>"` |
| `count` | Result cardinality or allow-list size | `EVENT_FILTERS="count=0"` |
| `latency_ms` | Upstream round-trip time (number) | read, do not filter |
| `status` | Upstream HTTP status (number) | `EVENT_FILTERS="status=429"` |

## Probes (model and credential)

Emitted by `internal/services/router/probe.go` (`Probing model succeeds`,
`Probing model reports quota exhaustion`, `Probing model fails`, `Probing
credential reports ...`). Quota exhaustion logs at info so a live but
limited model never reads as dead.

| Key | Meaning | Example filter |
|---|---|---|
| `model` | Full router model id under probe | `EVENT_FILTERS="model=google/gemini-2.5-pro-preview-tts"` |
| `code` | Probe verdict code (`rate_limit`, `unhealthy`, `unknown`, ...) | `EVENT_FILTERS="code=rate_limit"` |
| `reason` | Verdict reason (`quota_exhausted`, ...) | `EVENT_FILTERS="reason=quota_exhausted"` |
| `error` | Variable upstream text behind the verdict | read, do not filter |
| `latency_ms` | Probe round-trip time (number) | read, do not filter |
| `provider_id`, `type`, `cred_id` | Credential probe identity | `EVENT_FILTERS="cred_id=<id>"` |
| `operation` | Always `test_credential` on disable-path failures | `EVENT_FILTERS="operation=test_credential"` |

## Credentials

Emitted by `internal/services/credential/gorm_service.go` (`Credential
park succeeds`, `Credential creation succeeds`, `Credential disables by
...`) and `internal/services/luaplugin/capabilities.go:507-573`
(`Credentials listed`, `Credential parked`, ...).

| Key | Meaning | Example filter |
|---|---|---|
| `cred_id` | Credential identifier attempting or affected by the call | `EVENT_FILTERS="cred_id=<id>"` |
| `provider_id` | Owning provider instance | `EVENT_FILTERS="provider_id=google"` |
| `reason` | Variable cause (`gemini rate limited`, ...) | read, do not filter |
| `until` | Park expiry, RFC3339 (`Credential park succeeds` only) | read, do not filter |
| `wait` | Park or cooldown seconds (number) | read, do not filter |
| `operation` | Gated sub-operation (`generate_id`, `create`, `get`, `list_usable`, `disable_unhealthy`, `disable_by_plugin`, `require`, `query`, ...) | `EVENT_FILTERS="operation=disable_by_plugin"` |
| `count`, `total`, `disabled`, `parked`, `denied`, `expired` | Pool census numbers | `EVENT_FILTERS="parked=1"` |
| `error` | Variable failure text | read, do not filter |

## Proxy pool

Emitted by `internal/services/proxypool/reporter.go` (`Proxy pool event`,
`Proxy pool stats`, `Proxy lane stats`, `Proxy source stats`, `Proxy demand
stats`, `Proxy check`) and `pool.go`/`source.go` (`Querying proxies`,
`Requiring proxies ...`, `Fetching proxy feed ...`, `Marking proxy`).

| Key | Meaning | Example filter |
|---|---|---|
| `operation` | Pool event kind (`limit_change`, `net_change`, `background`, `ingest_done`, `warm`, `demand_priority`, ...) or query state | `EVENT_FILTERS="operation=limit_change"` |
| `count` | Pool, lane, queue, or waiter cardinality | `EVENT_FILTERS="count=0"` |
| `limit` | Pool concurrency limit | read, do not filter |
| `from`, `to` | Limit or network transitions (`degraded`/`good`, numbers) | `EVENT_FILTERS="to=good"` |
| `net` | Network quality (`good`, `degraded`) | `EVENT_FILTERS="net=degraded"` |
| `rtt_ms` | Measured round-trip on `net_change` (number) | read, do not filter |
| `lane` | Pool lane (`background`, `foreground`, `liveness`, `revival`) | `EVENT_FILTERS="lane=revival"` |
| `source` | Proxy feed name (`proxifly`, `monosans`, ...) | `EVENT_FILTERS="source=proxifly"` |
| `countries` | Demand country set (`any` or comma-joined ISO codes) | `EVENT_FILTERS="countries=US"` |
| `state` | Demand or pool state (`idle`, ...) | `EVENT_FILTERS="state=idle"` |
| `pool` | Custom pool id, or queried pool name | `EVENT_FILTERS="pool=auto"` |
| `country` | Queried country | `EVENT_FILTERS="country=US"` |
| `reason` | Ban-reason histogram or check failure | read, do not filter |
| `proxy_url` | Redacted endpoint under check or at fault | read, do not filter |
| `direct`, `timed_out` | Booleans on proxy require outcomes | `EVENT_FILTERS="timed_out"` |

## Plugin bridge

Persisted Lua `llm_router.log` lines carry the Lua-supplied keys plus
sandbox-attached `plugin_id` and `msg`
(`internal/services/luaplugin/sandbox.go:195`). Router-side plugin-service
lines (terminal errors, lifecycle) carry `plugin` (record id) and `type`
instead (`internal/services/luaplugin/service.go:663-683`). `plugin` and
`plugin_id` hold the same record id; filter whichever the line carries.
`plugin` and `type` are reserved and rejected in Lua keys.

| Key | Meaning | Example filter |
|---|---|---|
| `plugin` | Plugin record id on router-side lines | `EVENT_FILTERS="plugin=manual/TheSlopMachine/Google-AI-Studio"` |
| `plugin_id` | Same record id on Lua bridge lines | `EVENT_FILTERS="plugin_id=manual/TheSlopMachine/Google-AI-Studio"` |
| `type` | Plugin type key | `EVENT_FILTERS="type=google"` |
| `msg` | Exact Lua log message on bridge lines; exact match only, URL-encoded through `/api/events` | `/api/events?filter.msg=rate%20limited&limit=50` |
| `event` | High-volume debug category (`exits`, `rate_limit`, `forbidden`, `request_dump`, `response_dump`, `reasoning_replay`, `payload`, `cache`, `request_start`, `http`, `http_error`, `stream`, `stream_response`, `stream_error`, `stream_done`, `sse`) | `EVENT_FILTERS="event=stream_error"` |

Remaining Lua keys follow the `docs/PLUGIN-API.md` vocabulary (`status`,
`code`, `cred_id`, `provider_id`, `token_id`, `model`, `endpoint`, `count`,
`job`, `attempt`, `reason`, `detail`, `wait`, `latency_ms`, `exit`,
`proxy_url`, `body`, `line`, payload facts) plus per-plugin extras; filter
the stable ones, read the variable ones.

## Worked examples

One model exhausts quota; which other models failed right after, and did the
credential park or only cool down:

```
make log EVENT_FILTERS="code=rate_limit" EVENTS_LIMIT=50
make log EVENT_FILTERS="type=google" EVENTS_START="<window-start>" EVENTS_END="<window-end>"
make log EVENT_FILTERS="cred_id=<id>" EVENTS_LIMIT=20
make log EVENT_FILTERS="type=google !event=stream" EVENTS_LIMIT=50
```

Park rows (`Credential park succeeds` with `until`) mean every model on the
key stops; bare `rate limited` plugin lines with a `model` key and no park
mean only that key+model pair backs off.
