// Site-side wiring for the texts the widget library must not own.
import { setFuiTexts } from '../FUI/core/texts'
import { t } from './i18n.svelte'

setFuiTexts(() => ({
  close: t('common.actions.close'),
  confirm: t('common.actions.confirm'),
  cancel: t('common.actions.cancel'),
  searchPlaceholder: t('common.search.placeholder'),
  noOptions: t('common.empty.no_options'),
  copy: t('models.actions.copy_id'),
}))
