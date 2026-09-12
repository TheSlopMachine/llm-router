import { modal } from '../../lib/modal.svelte'
import PluginInstallModal from './PluginInstallModal.svelte'
import type { PluginFacts } from './plugin-facts'
import { t } from '../../lib/i18n.svelte'

// confirmInstall shows the permission modal and resolves true on confirm.
export function confirmInstall(title: string, facts: PluginFacts, confirmLabel: string): Promise<boolean> {
  return new Promise((resolve) => {
    modal.open({
      title,
      content: PluginInstallModal,
      severity: 'medium',
      size: 'small',
      props: { facts },
      buttons: [
        {
          label: t('Cancel'),
          variant: 'secondary',
          onClick: () => {
            modal.close()
            resolve(false)
          }
        },
        {
          label: confirmLabel,
          variant: 'primary',
          onClick: () => {
            modal.close()
            resolve(true)
          }
        }
      ]
    })
  })
}
