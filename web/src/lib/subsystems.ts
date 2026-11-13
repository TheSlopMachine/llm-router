import type { IconName } from '@fuckyouryan/fui'

// Icons of the router subsystems. The sidebar and every icon fallback read
// them from here.
export const PROVIDERS_ICON: IconName = 'cloud'
export const PROXIES_ICON: IconName = 'vpn_lock'
export const PLUGINS_ICON: IconName = 'extension'

const SUBSYSTEM_ICONS: Record<string, IconName> = {
  providers: PROVIDERS_ICON,
  proxies: PROXIES_ICON
}

/** Icon of the first known subsystem; the plugins icon when none is known. */
export function subsystemIcon(subsystems: readonly string[]): IconName {
  for (const subsystem of subsystems) {
    const icon = SUBSYSTEM_ICONS[subsystem]
    if (icon) return icon
  }
  return PLUGINS_ICON
}
