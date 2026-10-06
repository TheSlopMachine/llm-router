# i18n Robotic Keys

The dashboard uses categorized robotic keys for all UI text. The default UI shows English text. A key shows only when a translation is missing.

## Files

- `types.ts` - `TranslationKey` union. Add every new key here.
- `en.ts` - English source text. Add English text here.
- `ru.ts` - Russian translation. Add Russian text here.
- `index.ts` - Public API. Exports `t`, `n`, `tb`.

## API

```typescript
import { t, n, tb } from '$lib/i18n.svelte'

// Static text.
t('common.actions.save')

// Count plus noun. Russian forms resolve automatically.
n(count, 'time.units.minute.one', 'time.units.minute.many')

// Backend probe summary. Maps backend English to robotic keys.
tb(res.summary)
```

`tb` maps these backend summaries: `probe failed`, `authentication failed`,
`invalid request`, `quota exceeded, temporary`, `region blocked`,
`request timed out`, `upstream error`, `connection failed`,
`payment required`, `backend overloaded`, `health check not supported`,
`health check failed`, `credential unhealthy`,
`credential unhealthy, disabled`, `health check inconclusive`,
`disable failed`, `is healthy`, `failed`, `refreshed`.
Unknown input returns unchanged.

## Categories

Top level domains group keys by feature.

- `common` - Shared actions, labels, state, empty text, time words.
- `auth` - Sign in, sign out, username, password, errors.
- `nav` - Menu, search, settings, sidebar.
- `providers` - Provider list, detail, models, fields.
- `credentials` - Credential CRUD, test, probe state, summaries.
- `models` - Model list, search, filter, custom models, capabilities, endpoints.
- `tokens` - Token CRUD, wizard steps, permissions, status.
- `virtual` - Virtual model list, create, edit, decision routing.
- `plugins` - Tabs, catalog, installed, repos, install, update, rollback.
- `proxy` - Mode, pool status, sources, custom pools, retry mode.
- `settings` - Language, theme, accent, cluster, telemetry, password.
- `data` - Export, import, clear, purge, doctor.
- `metrics` - Time ranges, overview, peaks, totals.
- `upload` - File upload and inject position.
- `misc` - Delete confirm, copy model ID, agent text, step labels.
- `time` - Minute, hour, day, month, year units.
- `units` - Provider, model, credential, API call units.

Hierarchy is `<domain>.<subdomain>.<entity>.<suffix>`.
Depth is 2 to 4 levels. Example: `tokens.permissions.allow_all_models`.

## Key Syntax

- Use `snake_case` for every segment.
- Use full words. No abbreviations.
- Use ASCII only in English text. Use `...` not the Unicode ellipsis.
- Use `-` not em dash or en dash.
- Keep sentences short. Use simple structure.
- Title case for titles and buttons. Sentence case for descriptions.

Suffix conventions:

- `.title` - Page or modal title.
- `.label` - Form label.
- `.placeholder` - Input placeholder.
- `.hint` - Helper text under an input.
- `.action` - Button or link text under `common.actions` or `auth`.
- `.status` - Status indicator.
- `.error` - Error message.
- `.empty` - Empty state message.
- `.desc` - Description paragraph.
- `.one`, `.many` - English plural forms for `n`.
- `.one`, `.few`, `.many` - Russian plural forms for `n`.

## Examples

```typescript
'common.actions.save': 'Save',
'common.actions.cancel': 'Cancel',
'common.status.loading': 'Loading...',
'auth.sign_in.title': 'Sign in',
'auth.sign_out.action': 'Sign out',
'auth.errors.invalid_credentials': 'Invalid username or password.',
'providers.list.title': 'Providers',
'providers.list.empty': 'No providers yet. Add one to get started.',
'providers.detail.enable': 'Enable provider',
'credentials.test': 'Test credential',
'credentials.probe_failed': 'probe failed',
'models.list.title': 'Models',
'models.search.placeholder': 'Search models...',
'models.capabilities.vision': 'Vision',
'tokens.create.title': 'Create token',
'tokens.wizard.step_name': 'Name',
'tokens.permissions.allow_all_models': 'Allow all models',
'virtual.models_plural': 'Virtual models',
'virtual.enable_decision': 'Enable decision-based routing',
'plugins.tabs.catalog': 'Catalog',
'plugins.repo.add': 'Add repository',
'plugins.update.new_hosts': 'New hosts',
'proxy.mode.title': 'Proxy mode',
'proxy.pool.refresh': 'Refresh pool',
'settings.language.label': 'Language',
'settings.password.change': 'Change password',
'data.export': 'Export',
'data.doctor.title': 'Database Doctor',
'metrics.range.1day': '1 Day',
'metrics.total_requests': 'Total API Requests',
'time.units.minute.one': 'minute',
'time.units.minute.many': 'minutes',
'units.provider.one': 'provider',
'units.provider.many': 'providers',
'units.plugin.one': 'plugin',
'units.plugin.many': 'plugins',
'units.item.one': 'item',
'units.item.many': 'items',
```

Counts always use `n(count, ...one, ...many)`. Never render `{count} {t(...)}`
with a fixed noun: Russian plural forms depend on the count.

## Add a New Key

1. Add the key to the `TranslationKey` union in `types.ts`.
2. Add English text to `en.ts`. Use ASCII only.
3. Add Russian text to `ru.ts`.
4. Use the key in code: `t('domain.entity.suffix')`.
5. Run `make check-frontend`.

Missing keys return the key itself. This makes gaps visible during development.
