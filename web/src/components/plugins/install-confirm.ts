import { modal } from '../../lib/modal.svelte'
import PluginInstallModal from './PluginInstallModal.svelte'
import type { PluginFacts } from './plugin-facts'

// confirmInstall shows the permission modal and resolves true on confirm.
export function confirmInstall(title: string, facts: PluginFacts, confirmLabel: string): Promise<boolean> {
  return modal.confirm({
    title,
    content: PluginInstallModal,
    props: { facts },
    size: 'small',
    confirmText: confirmLabel,
  })
}
