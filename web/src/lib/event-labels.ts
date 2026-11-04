import type { TranslationKey } from './i18n/types'
import { t } from './i18n.svelte'

// Event keys the services log (docs/EVENT-KEYS.md) with a readable name; any other key shows as is.
const KEY_LABELS: Readonly<Record<string, TranslationKey>> = {
  subsystem: 'metrics.logs.key_subsystem.label',
  provider_id: 'metrics.logs.key_provider_id.label',
  type: 'metrics.logs.key_type.label',
  model: 'metrics.logs.key_model.label',
  endpoint: 'metrics.logs.key_endpoint.label',
  token_id: 'metrics.logs.key_token_id.label',
  code: 'metrics.logs.key_code.label',
  status: 'metrics.logs.key_status.label',
  operation: 'metrics.logs.key_operation.label',
  cred_id: 'metrics.logs.key_cred_id.label',
  plugin: 'metrics.logs.key_plugin.label',
  event: 'metrics.logs.key_event.label',
  source: 'metrics.logs.key_source.label',
  lane: 'metrics.logs.key_lane.label',
  pool: 'metrics.logs.key_pool.label',
  reason: 'metrics.logs.key_reason.label',
}

export function keyLabel(key: string): string {
  const label = KEY_LABELS[key]
  return label === undefined ? key : t(label)
}
